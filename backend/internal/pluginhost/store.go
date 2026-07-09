package pluginhost

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pluginkit"
)

const (
	// defaultPackageMaxBytes 是插件包的默认大小上限（64MB）：
	// 同时约束 zip 压缩包本体、解包后单文件与解包总量（zip 炸弹防线）。
	defaultPackageMaxBytes = 64 << 20
	// envPackageMaxMB 允许经环境变量调整上限（单位 MB，正整数）。
	envPackageMaxMB = "PLUGIN_PACKAGE_MAX_MB"
	// manifestMaxBytes 是 manifest.json 的单独上限，防清单本身撑爆内存。
	manifestMaxBytes = 1 << 20
	// extractMaxEntries 是解包条目数上限：字节上限拦不住海量空文件/空目录
	// 条目撑爆 inode 与目录项（条目数炸弹防线）。
	extractMaxEntries = 10_000
)

// ErrInvalidPackage 标记插件包本身不合法（结构/清单/大小/安全校验失败），
// 管理端点据此映射为 400。
var ErrInvalidPackage = errors.New("pluginhost: invalid plugin package")

// PackageStore 管理外部插件包的磁盘布局：<root>/<id>/<version>/…。
// root 通常为 DATA_DIR/plugins（见 DefaultStoreRoot）。
type PackageStore struct {
	root     string
	maxBytes int64
}

// DefaultStoreRoot 返回外部插件包的默认存储根目录 <DATA_DIR>/plugins。
// DATA_DIR 解析与 setup.GetDataDir 同一惯例（env DATA_DIR > /app/data 可写 > 当前目录）；
// setup 包经 repository 间接依赖本包，为避免 import cycle 在此复刻该解析。
func DefaultStoreRoot() string {
	return filepath.Join(resolveDataDir(), "plugins")
}

// resolveDataDir 按 setup.GetDataDir 的优先级解析数据目录。
func resolveDataDir() string {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		return dir
	}
	dockerDataDir := "/app/data"
	if info, err := os.Stat(dockerDataDir); err == nil && info.IsDir() {
		testFile := filepath.Join(dockerDataDir, ".write_test")
		if f, err := os.Create(testFile); err == nil {
			_ = f.Close()
			_ = os.Remove(testFile)
			return dockerDataDir
		}
	}
	return "."
}

// NewPackageStore 创建包存储；大小上限默认 64MB，可经 PLUGIN_PACKAGE_MAX_MB 调整。
func NewPackageStore(root string) *PackageStore {
	return newPackageStoreWithLimit(root, packageMaxBytesFromEnv())
}

// newPackageStoreWithLimit 供测试注入自定义上限。
func newPackageStoreWithLimit(root string, maxBytes int64) *PackageStore {
	return &PackageStore{root: root, maxBytes: maxBytes}
}

// packageMaxBytesFromEnv 解析环境变量上限；非法或缺省时用默认值。
func packageMaxBytesFromEnv() int64 {
	raw := strings.TrimSpace(os.Getenv(envPackageMaxMB))
	if raw == "" {
		return defaultPackageMaxBytes
	}
	mb, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || mb <= 0 {
		return defaultPackageMaxBytes
	}
	return mb << 20
}

// MaxBytes 返回插件包大小上限（handler 侧同步用于 multipart 上限）。
func (s *PackageStore) MaxBytes() int64 { return s.maxBytes }

// Root 返回存储根目录。
func (s *PackageStore) Root() string { return s.root }

// Dir 返回插件某版本的安装目录（不保证存在）。
func (s *PackageStore) Dir(id pluginkit.ID, version string) string {
	return filepath.Join(s.root, string(id), version)
}

// ReadManifest 从 zip 包根目录读取并解析 manifest.json（不做业务校验）。
func (s *PackageStore) ReadManifest(zipPath string) (*Manifest, error) {
	zr, err := s.openZip(zipPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name != ManifestFileName {
			continue
		}
		if f.UncompressedSize64 > manifestMaxBytes {
			return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidPackage, ManifestFileName, manifestMaxBytes)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: open %s: %v", ErrInvalidPackage, ManifestFileName, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, manifestMaxBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %v", ErrInvalidPackage, ManifestFileName, err)
		}
		if int64(len(data)) > manifestMaxBytes {
			return nil, fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidPackage, ManifestFileName, manifestMaxBytes)
		}
		m, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
		}
		return m, nil
	}
	return nil, fmt.Errorf("%w: %s not found at package root", ErrInvalidPackage, ManifestFileName)
}

// Extract 把 zip 包安全解包到 <root>/<id>/<version>/：
//   - 防 zip-slip：条目路径 Clean 后必须落在目标目录前缀内，拒绝绝对路径/../反斜杠/符号链接；
//   - 大小上限：单文件与解压总量均不得超过 MaxBytes（按实际写出字节计数，不信任 zip 头）；
//   - 原子落位：先解到同目录下的临时目录，全部成功后 rename 到版本目录
//     （已存在的同名版本目录先移除，覆盖语义供同版本重装）。
//
// 返回最终安装目录。
func (s *PackageStore) Extract(zipPath string, id pluginkit.ID, version string) (string, error) {
	zr, err := s.openZip(zipPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = zr.Close() }()

	idDir := filepath.Join(s.root, string(id))
	if err := os.MkdirAll(idDir, 0o755); err != nil {
		return "", fmt.Errorf("pluginhost: create plugin dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(idDir, ".extract-")
	if err != nil {
		return "", fmt.Errorf("pluginhost: create extract dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	if len(zr.File) > extractMaxEntries {
		cleanup()
		return "", fmt.Errorf("%w: too many entries (%d, limit %d)", ErrInvalidPackage, len(zr.File), extractMaxEntries)
	}
	var total int64
	for _, f := range zr.File {
		if err := s.extractEntry(f, tmpDir, &total); err != nil {
			cleanup()
			return "", err
		}
	}

	dest := filepath.Join(idDir, version)
	if err := os.RemoveAll(dest); err != nil {
		cleanup()
		return "", fmt.Errorf("pluginhost: remove existing version dir: %w", err)
	}
	if err := os.Rename(tmpDir, dest); err != nil {
		cleanup()
		return "", fmt.Errorf("pluginhost: finalize version dir: %w", err)
	}
	return dest, nil
}

// extractEntry 解出单个 zip 条目（目录或常规文件），累计 total 字节数。
func (s *PackageStore) extractEntry(f *zip.File, destRoot string, total *int64) error {
	target, err := s.secureJoin(destRoot, f.Name)
	if err != nil {
		return err
	}

	mode := f.Mode()
	switch {
	case f.FileInfo().IsDir():
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("pluginhost: create dir %s: %w", f.Name, err)
		}
		return nil
	case mode&os.ModeSymlink != 0:
		return fmt.Errorf("%w: symlink entry %q is not allowed", ErrInvalidPackage, f.Name)
	case !mode.IsRegular():
		return fmt.Errorf("%w: non-regular entry %q is not allowed", ErrInvalidPackage, f.Name)
	}

	if f.UncompressedSize64 > uint64(s.maxBytes) {
		return fmt.Errorf("%w: entry %q exceeds size limit %d bytes", ErrInvalidPackage, f.Name, s.maxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("pluginhost: create parent dir for %s: %w", f.Name, err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%w: open entry %q: %v", ErrInvalidPackage, f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("pluginhost: create file %s: %w", f.Name, err)
	}
	// 按实际写出字节计数施加单文件与总量上限，不信任 zip 头声明（zip 炸弹防线）。
	written, err := io.Copy(out, io.LimitReader(rc, s.maxBytes+1))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("pluginhost: write file %s: %w", f.Name, err)
	}
	if written > s.maxBytes {
		return fmt.Errorf("%w: entry %q exceeds size limit %d bytes", ErrInvalidPackage, f.Name, s.maxBytes)
	}
	*total += written
	if *total > s.maxBytes {
		return fmt.Errorf("%w: total uncompressed size exceeds limit %d bytes", ErrInvalidPackage, s.maxBytes)
	}
	return nil
}

// secureJoin 把 zip 条目名拼到解包根目录下，Clean 后强制前缀校验（防 zip-slip）。
func (s *PackageStore) secureJoin(destRoot, name string) (string, error) {
	if strings.Contains(name, `\`) {
		return "", fmt.Errorf("%w: entry %q must use forward slashes", ErrInvalidPackage, name)
	}
	target := filepath.Clean(filepath.Join(destRoot, filepath.FromSlash(name)))
	if target != destRoot && !strings.HasPrefix(target, destRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: entry %q escapes extraction root", ErrInvalidPackage, name)
	}
	return target, nil
}

// MarkExecutable 给安装目录内 manifest 声明的可执行文件加执行位（0755）。
// 仅处理包内实际存在的条目（跨平台包可只带当前平台的二进制）。
func (s *PackageStore) MarkExecutable(installDir string, m *Manifest) error {
	if m.Backend == nil {
		return nil
	}
	for platform, rel := range m.Backend.Executables {
		target := filepath.Join(installDir, filepath.FromSlash(rel))
		if _, err := os.Stat(target); err != nil {
			if os.IsNotExist(err) {
				if platform == CurrentPlatform() {
					return fmt.Errorf("%w: declared executable %q for %s missing from package", ErrInvalidPackage, rel, platform)
				}
				continue
			}
			return fmt.Errorf("pluginhost: stat executable %s: %w", rel, err)
		}
		if err := os.Chmod(target, 0o755); err != nil {
			return fmt.Errorf("pluginhost: chmod executable %s: %w", rel, err)
		}
	}
	return nil
}

// RemoveVersion 删除插件的指定版本目录（不存在时为 no-op）。
func (s *PackageStore) RemoveVersion(id pluginkit.ID, version string) error {
	if err := os.RemoveAll(s.Dir(id, version)); err != nil {
		return fmt.Errorf("pluginhost: remove version dir: %w", err)
	}
	return nil
}

// Remove 删除插件的整个存储目录（所有版本；不存在时为 no-op）。
func (s *PackageStore) Remove(id pluginkit.ID) error {
	if err := os.RemoveAll(filepath.Join(s.root, string(id))); err != nil {
		return fmt.Errorf("pluginhost: remove plugin dir: %w", err)
	}
	return nil
}

// openZip 打开 zip 包并校验压缩包本体大小上限。
func (s *PackageStore) openZip(zipPath string) (*zip.ReadCloser, error) {
	info, err := os.Stat(zipPath)
	if err != nil {
		return nil, fmt.Errorf("pluginhost: stat package: %w", err)
	}
	if info.Size() > s.maxBytes {
		return nil, fmt.Errorf("%w: package exceeds size limit %d bytes", ErrInvalidPackage, s.maxBytes)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("%w: not a valid zip archive: %v", ErrInvalidPackage, err)
	}
	return zr, nil
}

// FileSHA256 计算文件内容的 SHA256（hex），作为安装登记的校验和。
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("pluginhost: open package for checksum: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("pluginhost: hash package: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

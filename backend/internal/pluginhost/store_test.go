//go:build unit

package pluginhost

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// zipEntry 描述测试 zip 包的一个条目。
type zipEntry struct {
	name string
	body []byte
	mode os.FileMode // 0 = 常规文件默认权限
}

// writeTestZip 在临时目录生成一个 zip 包并返回其路径。
func writeTestZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pkg.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			hdr.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(hdr)
		require.NoError(t, err)
		_, err = w.Write(e.body)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

// testManifestBytes 生成含当前平台二进制声明的合法 manifest.json。
func testManifestBytes(t *testing.T, id, version string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id":       id,
		"name":     "Test Plugin",
		"version":  version,
		"protocol": ProtocolHTTP1,
		"backend": map[string]any{
			"executables": map[string]string{CurrentPlatform(): "bin/plugin"},
		},
	})
	require.NoError(t, err)
	return raw
}

// testPluginZip 生成一个可完整走通安装流程的插件包。
func testPluginZip(t *testing.T, id, version string) string {
	t.Helper()
	return writeTestZip(t, []zipEntry{
		{name: ManifestFileName, body: testManifestBytes(t, id, version)},
		{name: "bin/plugin", body: []byte("#!/bin/sh\necho " + version + "\n")},
		{name: "web/plugin.js", body: []byte("console.log('" + version + "')")},
	})
}

func newTestStore(t *testing.T) *PackageStore {
	t.Helper()
	return NewPackageStore(t.TempDir())
}

func TestPackageStoreReadManifest(t *testing.T) {
	store := newTestStore(t)
	zipPath := testPluginZip(t, "acme.hello", "1.0.0")

	m, err := store.ReadManifest(zipPath)
	require.NoError(t, err)
	require.Equal(t, "acme.hello", string(m.ID))
	require.Equal(t, "1.0.0", m.Version)
}

func TestPackageStoreReadManifestMissing(t *testing.T) {
	store := newTestStore(t)
	zipPath := writeTestZip(t, []zipEntry{{name: "bin/plugin", body: []byte("x")}})

	_, err := store.ReadManifest(zipPath)
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), ManifestFileName)
}

func TestPackageStoreReadManifestNotZip(t *testing.T) {
	store := newTestStore(t)
	path := filepath.Join(t.TempDir(), "not.zip")
	require.NoError(t, os.WriteFile(path, []byte("plain text"), 0o644))

	_, err := store.ReadManifest(path)
	require.ErrorIs(t, err, ErrInvalidPackage)
}

func TestPackageStoreExtractLayout(t *testing.T) {
	store := newTestStore(t)
	zipPath := testPluginZip(t, "acme.hello", "1.0.0")

	dir, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, store.Dir("acme.hello", "1.0.0"), dir)

	body, err := os.ReadFile(filepath.Join(dir, "web", "plugin.js"))
	require.NoError(t, err)
	require.Contains(t, string(body), "1.0.0")
	require.FileExists(t, filepath.Join(dir, ManifestFileName))
	require.FileExists(t, filepath.Join(dir, "bin", "plugin"))
}

// TestPackageStoreExtractReplacesExistingVersion 同版本重装：旧目录整体替换，无残留。
func TestPackageStoreExtractReplacesExistingVersion(t *testing.T) {
	store := newTestStore(t)
	dest := store.Dir("acme.hello", "1.0.0")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	stale := filepath.Join(dest, "stale.txt")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o644))

	_, err := store.Extract(testPluginZip(t, "acme.hello", "1.0.0"), "acme.hello", "1.0.0")
	require.NoError(t, err)
	require.NoFileExists(t, stale)
	require.FileExists(t, filepath.Join(dest, "bin", "plugin"))
}

func TestPackageStoreExtractRejectsZipSlip(t *testing.T) {
	store := newTestStore(t)
	cases := []struct {
		name  string
		entry string
	}{
		{"父目录穿越", "../evil.txt"},
		{"嵌套穿越", "a/../../evil.txt"},
		{"反斜杠路径", `..\evil.txt`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			zipPath := writeTestZip(t, []zipEntry{{name: tc.entry, body: []byte("evil")}})
			_, err := store.Extract(zipPath, "acme.hello", "1.0.0")
			require.ErrorIs(t, err, ErrInvalidPackage)
			// 逃逸文件不得落盘，版本目录也不得残留
			require.NoFileExists(t, filepath.Join(store.Root(), "evil.txt"))
			require.NoDirExists(t, store.Dir("acme.hello", "1.0.0"))
		})
	}
}

func TestPackageStoreExtractRejectsSymlink(t *testing.T) {
	store := newTestStore(t)
	zipPath := writeTestZip(t, []zipEntry{
		{name: "link", body: []byte("/etc/passwd"), mode: os.ModeSymlink | 0o777},
	})
	_, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), "symlink")
}

func TestPackageStoreExtractEnforcesSingleFileLimit(t *testing.T) {
	// 高压缩比数据：压缩包本体在上限内，单文件解压后超限。
	store := newPackageStoreWithLimit(t.TempDir(), 1000)
	zipPath := writeTestZip(t, []zipEntry{
		{name: "big.bin", body: bytes.Repeat([]byte("a"), 1500)},
	})
	_, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), `entry "big.bin" exceeds size limit`)
}

func TestPackageStoreExtractEnforcesTotalLimit(t *testing.T) {
	// 高压缩比数据：压缩包本体远小于上限，但解压总量超限（zip 炸弹形态）。
	store := newPackageStoreWithLimit(t.TempDir(), 1000)
	zipPath := writeTestZip(t, []zipEntry{
		{name: "a.bin", body: bytes.Repeat([]byte("a"), 900)},
		{name: "b.bin", body: bytes.Repeat([]byte("b"), 900)},
	})
	_, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), "total uncompressed size")
}

func TestPackageStoreExtractEnforcesEntryCountLimit(t *testing.T) {
	// 海量空条目：字节上限完全拦不住（0 字节），靠条目数上限拒绝（inode 炸弹形态）。
	store := newTestStore(t)
	entries := make([]zipEntry, 0, extractMaxEntries+1)
	for i := 0; i <= extractMaxEntries; i++ {
		entries = append(entries, zipEntry{name: fmt.Sprintf("e/%d", i)})
	}
	zipPath := writeTestZip(t, entries)
	_, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), "too many entries")
}

func TestPackageStoreRejectsOversizedArchive(t *testing.T) {
	store := newPackageStoreWithLimit(t.TempDir(), 16)
	zipPath := testPluginZip(t, "acme.hello", "1.0.0")
	_, err := store.ReadManifest(zipPath)
	require.ErrorIs(t, err, ErrInvalidPackage)
	require.Contains(t, err.Error(), "package exceeds size limit")
}

func TestPackageStoreMarkExecutable(t *testing.T) {
	store := newTestStore(t)
	zipPath := testPluginZip(t, "acme.hello", "1.0.0")
	dir, err := store.Extract(zipPath, "acme.hello", "1.0.0")
	require.NoError(t, err)

	m, err := store.ReadManifest(zipPath)
	require.NoError(t, err)
	require.NoError(t, store.MarkExecutable(dir, m))

	info, err := os.Stat(filepath.Join(dir, "bin", "plugin"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	// 声明了当前平台但包内缺失 → ErrInvalidPackage
	m.Backend.Executables[CurrentPlatform()] = "bin/missing"
	err = store.MarkExecutable(dir, m)
	require.ErrorIs(t, err, ErrInvalidPackage)

	// 其他平台的二进制缺失可容忍（跨平台包可只带当前平台）
	m.Backend.Executables = map[string]string{
		CurrentPlatform(): "bin/plugin",
		"plan9-mips":      "bin/other",
	}
	require.NoError(t, store.MarkExecutable(dir, m))
}

func TestPackageStoreRemove(t *testing.T) {
	store := newTestStore(t)
	for _, version := range []string{"1.0.0", "2.0.0"} {
		_, err := store.Extract(testPluginZip(t, "acme.hello", version), "acme.hello", version)
		require.NoError(t, err)
	}

	require.NoError(t, store.RemoveVersion("acme.hello", "1.0.0"))
	require.NoDirExists(t, store.Dir("acme.hello", "1.0.0"))
	require.DirExists(t, store.Dir("acme.hello", "2.0.0"))

	require.NoError(t, store.Remove("acme.hello"))
	require.NoDirExists(t, filepath.Join(store.Root(), "acme.hello"))

	// 不存在时为 no-op
	require.NoError(t, store.Remove("acme.hello"))
	require.NoError(t, store.RemoveVersion("acme.hello", "9.9.9"))
}

func TestPackageMaxBytesFromEnv(t *testing.T) {
	t.Setenv(envPackageMaxMB, "1")
	require.Equal(t, int64(1<<20), packageMaxBytesFromEnv())

	t.Setenv(envPackageMaxMB, "not-a-number")
	require.Equal(t, int64(defaultPackageMaxBytes), packageMaxBytesFromEnv())

	t.Setenv(envPackageMaxMB, "-3")
	require.Equal(t, int64(defaultPackageMaxBytes), packageMaxBytesFromEnv())

	t.Setenv(envPackageMaxMB, "")
	require.Equal(t, int64(defaultPackageMaxBytes), packageMaxBytesFromEnv())
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
	sum, err := FileSHA256(path)
	require.NoError(t, err)
	// echo -n hello | sha256sum
	require.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", sum)

	_, err = FileSHA256(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrInvalidPackage), fmt.Sprintf("IO 错误不应标记为包不合法: %v", err))
}

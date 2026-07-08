//go:build unit

package pluginkit

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestParsePluginsConfig_NilAndEmpty(t *testing.T) {
	cfg, err := ParsePluginsConfig(nil)
	require.NoError(t, err)
	require.Empty(t, cfg)

	cfg, err = ParsePluginsConfig(map[string]any{})
	require.NoError(t, err)
	require.Empty(t, cfg)
}

func TestParsePluginsConfig_NonMapRoot(t *testing.T) {
	_, err := ParsePluginsConfig("not-a-map")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a map")
}

func TestParsePluginsConfig_SimpleID(t *testing.T) {
	cfg, err := ParsePluginsConfig(map[string]any{
		"demo": map[string]any{"greeting": "hi"},
	})
	require.NoError(t, err)
	require.Len(t, cfg, 1)
	require.Equal(t, map[string]any{"greeting": "hi"}, cfg[ID("demo")])
}

func TestParsePluginsConfig_ViperSplitNested(t *testing.T) {
	// viper 会把 "plugins.job.backup.greeting" 拆层为 job→backup→greeting，
	// 解析需还原点分 ID "job.backup"。
	cfg, err := ParsePluginsConfig(map[string]any{
		"job": map[string]any{
			"backup": map[string]any{"greeting": "hi", "interval": "5s"},
		},
	})
	require.NoError(t, err)
	require.Len(t, cfg, 1)
	require.Equal(t, map[string]any{"greeting": "hi", "interval": "5s"}, cfg[ID("job.backup")])
}

func TestParsePluginsConfig_LiteralDottedKey(t *testing.T) {
	cfg, err := ParsePluginsConfig(map[string]any{
		"job.backup": map[string]any{"greeting": "hi"},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"greeting": "hi"}, cfg[ID("job.backup")])
}

func TestParsePluginsConfig_LiteralDottedKeyStopsDescent(t *testing.T) {
	// 字面点分 key 视为完整插件 ID，即使其值全是嵌套 map 也不再下钻。
	cfg, err := ParsePluginsConfig(map[string]any{
		"job.backup": map[string]any{
			"limits": map[string]any{"max": 5},
		},
	})
	require.NoError(t, err)
	require.Len(t, cfg, 1)
	require.Equal(t, map[string]any{"limits": map[string]any{"max": 5}}, cfg[ID("job.backup")])
}

func TestParsePluginsConfig_NamespaceWithMultiplePlugins(t *testing.T) {
	cfg, err := ParsePluginsConfig(map[string]any{
		"job": map[string]any{
			"backup":  map[string]any{"greeting": "hi"},
			"archive": map[string]any{"keep": 3},
		},
		"demo": map[string]any{"greeting": "yo"},
	})
	require.NoError(t, err)
	require.Len(t, cfg, 3)
	require.Contains(t, cfg, ID("job.backup"))
	require.Contains(t, cfg, ID("job.archive"))
	require.Contains(t, cfg, ID("demo"))
}

func TestParsePluginsConfig_DuplicateAmbiguous(t *testing.T) {
	// 同一 ID 同时以字面 key 与拆层形态出现 → 报错而非静默合并。
	_, err := ParsePluginsConfig(map[string]any{
		"job.backup": map[string]any{"greeting": "a"},
		"job": map[string]any{
			"backup": map[string]any{"greeting": "b"},
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}

func TestParsePluginsConfig_InvalidID(t *testing.T) {
	_, err := ParsePluginsConfig(map[string]any{
		"bad_id": map[string]any{"x": 1},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid plugin id")
}

func TestParsePluginsConfig_ScalarAtPluginLevel(t *testing.T) {
	_, err := ParsePluginsConfig(map[string]any{"demo": "hi"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a map")
}

func TestParsePluginsConfig_NilStanzaAndEmptyMap(t *testing.T) {
	// yaml 中 "demo:" 空段落与显式空 map 都是"声明存在但无配置"。
	cfg, err := ParsePluginsConfig(map[string]any{
		"demo":  nil,
		"other": map[string]any{},
	})
	require.NoError(t, err)
	require.Len(t, cfg, 2)
	require.Empty(t, cfg[ID("demo")])
	require.Empty(t, cfg[ID("other")])
}

func TestParsePluginsConfig_AmbiguityCharacterization(t *testing.T) {
	// 已知局限（特征化）：插件 "job" 的私有配置顶层若全是嵌套 map，
	// 会被误还原为更深的 ID。规避方式见 ParsePluginsConfig 注释。
	cfg, err := ParsePluginsConfig(map[string]any{
		"job": map[string]any{
			"limits": map[string]any{"max": 5},
		},
	})
	require.NoError(t, err)
	require.NotContains(t, cfg, ID("job"))
	require.Contains(t, cfg, ID("job.limits"))
}

func TestParsePluginsConfig_ViperIntegration(t *testing.T) {
	// 用真实 viper 钉住两种形态的原始输入假设。
	t.Run("yaml literal dotted key", func(t *testing.T) {
		v := viper.New()
		v.SetConfigType("yaml")
		require.NoError(t, v.ReadConfig(strings.NewReader(`
plugins:
  demo:
    greeting: hello
  job.backup:
    interval: 5s
`)))
		cfg, err := ParsePluginsConfig(v.Get("plugins"))
		require.NoError(t, err)
		require.Len(t, cfg, 2)
		require.Equal(t, "hello", cfg[ID("demo")]["greeting"])
		require.Equal(t, "5s", cfg[ID("job.backup")]["interval"])
	})

	t.Run("viper.Set split nesting", func(t *testing.T) {
		v := viper.New()
		v.Set("plugins.job.backup.greeting", "hi")
		cfg, err := ParsePluginsConfig(v.Get("plugins"))
		require.NoError(t, err)
		require.Len(t, cfg, 1)
		require.Equal(t, "hi", cfg[ID("job.backup")]["greeting"])
	})
}

type demoPluginConfig struct {
	Greeting string        `mapstructure:"greeting"`
	Interval time.Duration `mapstructure:"interval"`
	Limit    int           `mapstructure:"limit"`
}

func TestDecoderFor_UnconfiguredZeroValue(t *testing.T) {
	cfg := PluginsConfig{}
	var out demoPluginConfig
	require.NoError(t, cfg.DecoderFor("demo")(&out))
	require.Equal(t, demoPluginConfig{}, out)
}

func TestDecoderFor_Decode(t *testing.T) {
	cfg := PluginsConfig{
		"demo": map[string]any{
			"greeting": "hi",
			"interval": "5s", // duration hook
			"limit":    "42", // 弱类型转换，对齐 viper 行为
		},
	}
	var out demoPluginConfig
	require.NoError(t, cfg.DecoderFor("demo")(&out))
	require.Equal(t, "hi", out.Greeting)
	require.Equal(t, 5*time.Second, out.Interval)
	require.Equal(t, 42, out.Limit)
}

func TestDecoderFor_TypeError(t *testing.T) {
	cfg := PluginsConfig{
		"demo": map[string]any{
			"limit": map[string]any{"nested": true},
		},
	}
	var out demoPluginConfig
	err := cfg.DecoderFor("demo")(&out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "plugins.demo")
}

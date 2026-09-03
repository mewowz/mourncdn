package main

import (
	"reflect"

	"github.com/dustin/go-humanize"
	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const DefaultConfigPath = "./config.yml"

type Config struct {
	ServeCfg  localAssetServerConfig
	UploadCfg localAssetUploaderConfig
	HTTPCfg   CDNServerConfig
}

func defaultKoanfConfig() (*koanf.Koanf, error) {
	defaults := map[string]any{
		"cdn.serve-route":                  "/assets",
		"cdn.upload-route":                 "/upload",
		"cdn.address":                      "localhost:9743",
		"cdn.advanced.read-timeout":        "15s",
		"cdn.advanced.read-header-timeout": "3s",
		"cdn.advanced.write-timeout":       "15s",
		"cdn.advanced.idle-timeout":        "60s",

		"serve.asset-dir":                  "./data/assets",
		"serve.max-cacheable-size":         "100MiB",
		"serve.cache-size":                 "1GiB",
		"serve.asset-ttl":                  "60s",
		"serve.advanced.write-buffer-size": 4096,
		"serve.advanced.write-window":      "10s",

		"upload.temp-dir":        "./data/tmp",
		"upload.output-dir":      "./data/assets",
		"upload.url-prefix":      "/assets",
		"upload.max-upload-size": "2GiB",
	}

	k := koanf.New(".")
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return nil, err
	}

	return k, nil
}

func LoadConfigFile(path string) (*Config, error) {
	k, err := defaultKoanfConfig()
	if err != nil {
		return nil, err
	}
	f := file.Provider(path)
	if err := k.Load(f, yaml.Parser()); err != nil {
		return nil, err
	}

	dc := mapstructure.DecoderConfig{
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			byteSizeHook(),
		),
	}
	uc := koanf.UnmarshalConf{
		Tag:           "koanf",
		DecoderConfig: &dc,
		FlatPaths:     true,
	}

	var serveCfg localAssetServerConfig
	if err := k.UnmarshalWithConf("serve", &serveCfg, uc); err != nil {
		return nil, err
	}

	var uploadCfg localAssetUploaderConfig
	if err := k.UnmarshalWithConf("upload", &uploadCfg, uc); err != nil {
		return nil, err
	}

	var cdnCfg CDNServerConfig
	if err := k.UnmarshalWithConf("cdn", &cdnCfg, uc); err != nil {
		return nil, err
	}

	return &Config{
		ServeCfg:  serveCfg,
		UploadCfg: uploadCfg,
		HTTPCfg:   cdnCfg,
	}, nil
}

func byteSizeHook() mapstructure.DecodeHookFunc {
	return byteSizeHookFunc
}

func byteSizeHookFunc(
	from reflect.Type,
	to reflect.Type,
	data any,
) (any, error) {
	if from.Kind() != reflect.String {
		return data, nil
	}

	if to.Kind() != reflect.Int && to.Kind() != reflect.Int64 {
		return data, nil
	}

	n, err := humanize.ParseBytes(data.(string))
	if err != nil {
		return nil, err
	}

	if to.Kind() == reflect.Int {
		return int(n), nil
	}

	return int64(n), nil
}

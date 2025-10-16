package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/ini.v1"
	"liuproxy_nexus/internal/shared/types"
)

// LoadIni 只加载 vless.ini 行为配置文件。
func LoadIni(cfg *types.Config, fileName string) error {
	iniFile, err := ini.Load(fileName)
	if err != nil {
		return err
	}
	if err := iniFile.MapTo(cfg); err != nil {
		return err
	}
	overrideFromEnvInt(&cfg.CommonConf.Crypt, "CRYPT_KEY")
	return nil
}

// LoadServers 加载 servers.json 数据文件。
func LoadServers(fileName string) ([]*types.ServerProfile, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		// 如果文件不存在，返回一个空列表而不是错误
		if os.IsNotExist(err) {
			return []*types.ServerProfile{}, nil
		}
		return nil, fmt.Errorf("failed to read servers file: %w", err)
	}

	var profiles []*types.ServerProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, fmt.Errorf("failed to unmarshal servers.json: %w", err)
	}
	return profiles, nil
}

// SaveServers 将服务器配置列表保存到 servers.json。
func SaveServers(fileName string, profiles []*types.ServerProfile) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal server profiles: %w", err)
	}
	return os.WriteFile(fileName, data, 0644)
}

func overrideFromEnvInt(target *int, envName string) {
	envValue := os.Getenv(envName)
	if envValue != "" {
		if intValue, err := strconv.Atoi(envValue); err == nil {
			*target = intValue
		}
	}
}

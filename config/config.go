package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hoshinonyaruko/gensokyo-mcp/mylog"
	"github.com/hoshinonyaruko/gensokyo-mcp/structs"
	"github.com/hoshinonyaruko/gensokyo-mcp/sys"
	"github.com/hoshinonyaruko/gensokyo-mcp/template"
	"gopkg.in/yaml.v3"
)

// 不支持配置热重载的配置项
var restartRequiredFields = []string{
	"WsAddress", "WsToken", "ReconnectTimes", "HeartBeatInterval", "LaunchReconnectTimes",
}

var (
	instance *Config
	mu       sync.RWMutex
)

type Config struct {
	Version  int              `yaml:"version"`
	Settings structs.Settings `yaml:"settings"`
}

// CommentInfo 用于存储注释及其定位信息
type CommentBlock struct {
	Comments  []string // 一个或多个连续的注释
	TargetKey string   // 注释所指向的键（如果有）
	Offset    int      // 注释与目标键之间的行数
}

// LoadConfig 从文件中加载配置并初始化单例配置
func LoadConfig(path string, fastload bool) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()

	configData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 检查并替换视觉前缀行，如果有必要，后期会注释
	// var isChange bool
	// configData, isChange = replaceVisualPrefixsLine(configData)
	// if isChange {
	// 	// 如果配置文件已修改，重新写入修正后的数据
	// 	if err = os.WriteFile(path, configData, 0644); err != nil {
	// 		return nil, err // 处理写入错误
	// 	}
	// }

	// 尝试解析配置数据
	conf := &Config{}
	if err = yaml.Unmarshal(configData, conf); err != nil {
		return nil, err
	}

	if !fastload {
		// 确保本地配置文件的完整性,添加新的字段
		if err = ensureConfigComplete(path); err != nil {
			return nil, err
		}
	} else {
		if isValidConfig(conf) {
			//log.Printf("instance.Settings：%v", instance.Settings)
			// 用现有的instance比对即将覆盖赋值的conf,用[]string返回配置发生了变化的配置项
			changedFields := compareConfigChanges("Settings", instance.Settings, conf.Settings)
			// 根据changedFields进行进一步的操作，在不支持热重载的字段实现自动重启
			if len(changedFields) > 0 {
				log.Printf("配置已变更的字段：%v", changedFields)
				checkForRestart(changedFields) // 检查变更字段是否需要重启
			}
		} //conf为空时不对比
	}

	// 更新单例实例，即使它已经存在 更新前检查是否有效,vscode对文件的更新行为会触发2次文件变动
	// 第一次会让configData为空,迅速的第二次才是正常有值的configData
	if isValidConfig(conf) {
		instance = conf
	}

	return instance, nil
}

func isValidConfig(conf *Config) bool {
	// 确认config不为空且必要字段已设置
	return conf != nil && conf.Version != 0
}

// 去除Settings前缀
func stripSettingsPrefix(fieldName string) string {
	return strings.TrimPrefix(fieldName, "Settings.")
}

// compareConfigChanges 检查并返回发生变化的配置字段，处理嵌套结构体
func compareConfigChanges(prefix string, oldConfig interface{}, newConfig interface{}) []string {
	var changedFields []string

	oldVal := reflect.ValueOf(oldConfig)
	newVal := reflect.ValueOf(newConfig)

	// 解引用指针
	if oldVal.Kind() == reflect.Ptr {
		oldVal = oldVal.Elem()
	}
	if newVal.Kind() == reflect.Ptr {
		newVal = newVal.Elem()
	}

	// 遍历所有字段
	for i := 0; i < oldVal.NumField(); i++ {
		oldField := oldVal.Field(i)
		newField := newVal.Field(i)
		fieldType := oldVal.Type().Field(i)
		fieldName := fieldType.Name

		fullFieldName := fieldName
		if prefix != "" {
			fullFieldName = fmt.Sprintf("%s.%s", prefix, fieldName)
		}

		// 对于结构体字段递归比较
		if oldField.Kind() == reflect.Struct || newField.Kind() == reflect.Struct {
			subChanges := compareConfigChanges(fullFieldName, oldField.Interface(), newField.Interface())
			changedFields = append(changedFields, subChanges...)
		} else {
			// 打印将要比较的字段和它们的值
			//fmt.Printf("Comparing field: %s\nOld value: %v\nNew value: %v\n", fullFieldName, oldField.Interface(), newField.Interface())
			if !reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
				//fmt.Println("-> Field changed")
				// 去除Settings前缀后添加到变更字段列表
				changedField := stripSettingsPrefix(fullFieldName)
				changedFields = append(changedFields, changedField)
			}
		}
	}

	return changedFields
}

// 检查是否需要重启
func checkForRestart(changedFields []string) {
	for _, field := range changedFields {
		for _, restartField := range restartRequiredFields {
			if field == restartField {
				fmt.Println("Configuration change requires restart:", field)
				sys.RestartApplication() // 调用重启函数
				return
			}
		}
	}
}

func CreateAndWriteConfigTemp() error {
	// 读取config.yml
	configFile, err := os.ReadFile("config.yml")
	if err != nil {
		return err
	}

	// 获取当前日期
	currentDate := time.Now().Format("2006-1-2")
	// 重命名原始config.yml文件
	err = os.Rename("config.yml", "config"+currentDate+".yml")
	if err != nil {
		return err
	}

	var config Config
	err = yaml.Unmarshal(configFile, &config)
	if err != nil {
		return err
	}

	// 创建config_temp.yml文件
	tempFile, err := os.Create("config.yml")
	if err != nil {
		return err
	}
	defer tempFile.Close()

	// 使用yaml.Encoder写入，以保留注释
	encoder := yaml.NewEncoder(tempFile)
	encoder.SetIndent(2) // 设置缩进
	err = encoder.Encode(config)
	if err != nil {
		return err
	}

	// 处理注释并重命名文件
	err = addCommentsToConfigTemp(template.ConfigTemplate, "config.yml")
	if err != nil {
		return err
	}

	return nil
}

func parseTemplate(template string) ([]CommentBlock, map[string]string) {
	var blocks []CommentBlock
	lines := strings.Split(template, "\n")

	var currentBlock CommentBlock
	var lastKey string

	directComments := make(map[string]string)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			currentBlock.Comments = append(currentBlock.Comments, trimmed) // 收集注释行
		} else {
			if containsKey(trimmed) {
				key := strings.SplitN(trimmed, ":", 2)[0]
				trimmedKey := strings.TrimSpace(key)

				if len(currentBlock.Comments) > 0 {
					currentBlock.TargetKey = lastKey // 关联到上一个找到的键
					blocks = append(blocks, currentBlock)
					currentBlock = CommentBlock{} // 重置为新的注释块
				}

				// 如果当前行包含注释，则单独处理
				if parts := strings.SplitN(trimmed, "#", 2); len(parts) > 1 {
					directComments[trimmedKey] = "#" + parts[1]
				}
				lastKey = trimmedKey // 更新最后一个键
			} else if len(currentBlock.Comments) > 0 {
				// 如果当前行不是注释行且存在挂起的注释，但并没有新的键出现，将其作为独立的注释块
				blocks = append(blocks, currentBlock)
				currentBlock = CommentBlock{} // 重置为新的注释块
			}
		}
	}

	// 处理文件末尾的挂起注释块
	if len(currentBlock.Comments) > 0 {
		blocks = append(blocks, currentBlock)
	}

	return blocks, directComments
}

func addCommentsToConfigTemp(template, tempFilePath string) error {
	commentBlocks, directComments := parseTemplate(template)
	//fmt.Printf("%v\n", directComments)

	// 读取并分割新生成的配置文件内容
	content, err := os.ReadFile(tempFilePath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")

	// 处理并插入注释
	for _, block := range commentBlocks {
		// 根据注释块的目标键，找到插入位置并插入注释
		for i, line := range lines {
			if containsKey(line) {
				key := strings.SplitN(line, ":", 2)[0]
				if strings.TrimSpace(key) == block.TargetKey {
					// 计算基本插入点：在目标键之后
					insertionPoint := i + block.Offset + 1

					// 向下移动插入点直到找到键行或到达文件末尾
					for insertionPoint < len(lines) && !containsKey(lines[insertionPoint]) {
						insertionPoint++
					}

					// 在计算出的插入点插入注释
					if insertionPoint >= len(lines) {
						lines = append(lines, block.Comments...) // 如果到达文件末尾，直接追加注释
					} else {
						// 插入注释到计算出的位置
						lines = append(lines[:insertionPoint], append(block.Comments, lines[insertionPoint:]...)...)
					}
					break
				}
			}
		}
	}

	// 处理直接跟在键后面的注释
	// 接着处理直接跟在键后面的注释
	for i, line := range lines {
		if containsKey(line) {
			key := strings.SplitN(line, ":", 2)[0]
			trimmedKey := strings.TrimSpace(key)
			//fmt.Printf("%v\n", trimmedKey)
			if comment, exists := directComments[trimmedKey]; exists {
				// 如果这个键有直接的注释
				lines[i] = line + " " + comment
			}
		}
	}

	// 重新组合lines为一个字符串，准备写回文件
	updatedContent := strings.Join(lines, "\n")

	// 写回更新后的内容到原配置文件
	err = os.WriteFile(tempFilePath, []byte(updatedContent), 0644)
	if err != nil {
		return err
	}

	return nil
}

// containsKey 检查给定的字符串行是否可能包含YAML键。
// 它尝试排除注释行和冒号用于其他目的的行（例如，在URLs中）。
func containsKey(line string) bool {
	// 去除行首和行尾的空格
	trimmedLine := strings.TrimSpace(line)

	// 如果行是注释，直接返回false
	if strings.HasPrefix(trimmedLine, "#") {
		return false
	}

	// 检查是否存在冒号，如果不存在，则直接返回false
	colonIndex := strings.Index(trimmedLine, ":")
	return colonIndex != -1
}

// 确保配置完整性
func ensureConfigComplete(path string) error {
	// 读取配置文件到缓冲区
	configData, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// 将现有的配置解析到结构体中
	currentConfig := &Config{}
	err = yaml.Unmarshal(configData, currentConfig)
	if err != nil {
		return err
	}

	// 解析默认配置模板到结构体中
	defaultConfig := &Config{}
	err = yaml.Unmarshal([]byte(template.ConfigTemplate), defaultConfig)
	if err != nil {
		return err
	}

	// 使用反射找出结构体中缺失的设置
	missingSettingsByReflection, err := getMissingSettingsByReflection(currentConfig, defaultConfig)
	if err != nil {
		return err
	}

	// 使用文本比对找出缺失的设置
	missingSettingsByText, err := getMissingSettingsByText(template.ConfigTemplate, string(configData))
	if err != nil {
		return err
	}

	// 合并缺失的设置
	allMissingSettings := mergeMissingSettings(missingSettingsByReflection, missingSettingsByText)

	// 如果存在缺失的设置，处理缺失的配置行
	if len(allMissingSettings) > 0 {
		fmt.Println("缺失的设置:", allMissingSettings)
		missingConfigLines, err := extractMissingConfigLines(allMissingSettings, template.ConfigTemplate)
		if err != nil {
			return err
		}

		// 将缺失的配置追加到现有配置文件
		err = appendToConfigFile(path, missingConfigLines)
		if err != nil {
			return err
		}

		fmt.Println("检测到配置文件缺少项。已经更新配置文件，正在重启程序以应用新的配置。")
		sys.RestartApplication()
	}

	return nil
}

// mergeMissingSettings 合并由反射和文本比对找到的缺失设置
func mergeMissingSettings(reflectionSettings, textSettings map[string]string) map[string]string {
	for k, v := range textSettings {
		reflectionSettings[k] = v
	}
	return reflectionSettings
}

// getMissingSettingsByReflection 使用反射来对比结构体并找出缺失的设置
func getMissingSettingsByReflection(currentConfig, defaultConfig *Config) (map[string]string, error) {
	missingSettings := make(map[string]string)
	currentVal := reflect.ValueOf(currentConfig).Elem()
	defaultVal := reflect.ValueOf(defaultConfig).Elem()

	for i := 0; i < currentVal.NumField(); i++ {
		field := currentVal.Type().Field(i)
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "" || field.Type.Kind() == reflect.Int || field.Type.Kind() == reflect.Bool {
			continue // 跳过没有yaml标签的字段，或者字段类型为int或bool
		}
		yamlKeyName := strings.SplitN(yamlTag, ",", 2)[0]
		if isZeroOfUnderlyingType(currentVal.Field(i).Interface()) && !isZeroOfUnderlyingType(defaultVal.Field(i).Interface()) {
			missingSettings[yamlKeyName] = "missing"
		}
	}

	return missingSettings, nil
}

// getMissingSettingsByText compares settings in two strings line by line, looking for missing keys.
func getMissingSettingsByText(templateContent, currentConfigContent string) (map[string]string, error) {
	templateKeys := extractKeysFromString(templateContent)
	currentKeys := extractKeysFromString(currentConfigContent)

	missingSettings := make(map[string]string)
	for key := range templateKeys {
		if _, found := currentKeys[key]; !found {
			missingSettings[key] = "missing"
		}
	}

	return missingSettings, nil
}

// extractKeysFromString reads a string and extracts the keys (text before the colon).
func extractKeysFromString(content string) map[string]bool {
	keys := make(map[string]bool)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, ":") {
			key := strings.TrimSpace(strings.Split(line, ":")[0])
			keys[key] = true
		}
	}
	return keys
}

func extractMissingConfigLines(missingSettings map[string]string, configTemplate string) ([]string, error) {
	var missingConfigLines []string

	lines := strings.Split(configTemplate, "\n")
	for yamlKey := range missingSettings {
		found := false
		// Create a regex to match the line with optional spaces around the colon
		regexPattern := fmt.Sprintf(`^\s*%s\s*:\s*`, regexp.QuoteMeta(yamlKey))
		regex, err := regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %s", err)
		}

		for _, line := range lines {
			if regex.MatchString(line) {
				missingConfigLines = append(missingConfigLines, line)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("missing configuration for key: %s", yamlKey)
		}
	}

	return missingConfigLines, nil
}

func appendToConfigFile(path string, lines []string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Println("打开文件错误:", err)
		return err
	}
	defer file.Close()

	// 写入缺失的配置项
	for _, line := range lines {
		if _, err := file.WriteString("\n" + line); err != nil {
			fmt.Println("写入配置错误:", err)
			return err
		}
	}

	// 输出写入状态
	fmt.Println("配置已更新，写入到文件:", path)

	return nil
}

func isZeroOfUnderlyingType(x interface{}) bool {
	return reflect.DeepEqual(x, reflect.Zero(reflect.TypeOf(x)).Interface())
}

// UpdateConfig 将配置写入文件
func UpdateConfig(conf *Config, path string) error {
	data, err := yaml.Marshal(conf)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// WriteYAMLToFile 将YAML格式的字符串写入到指定的文件路径
func WriteYAMLToFile(yamlContent string) error {
	// 获取当前执行的可执行文件的路径
	exePath, err := os.Executable()
	if err != nil {
		log.Println("Error getting executable path:", err)
		return err
	}

	// 获取可执行文件所在的目录
	exeDir := filepath.Dir(exePath)

	// 构建config.yml的完整路径
	configPath := filepath.Join(exeDir, "config.yml")

	// 写入文件
	os.WriteFile(configPath, []byte(yamlContent), 0644)

	sys.RestartApplication()
	return nil
}

// DeleteConfig 删除配置文件并创建一个新的配置文件模板
func DeleteConfig() error {
	// 获取当前执行的可执行文件的路径
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error getting executable path:", err)
		return err
	}

	// 获取可执行文件所在的目录
	exeDir := filepath.Dir(exePath)

	// 构建config.yml的完整路径
	configPath := filepath.Join(exeDir, "config.yml")

	// 删除配置文件
	if err := os.Remove(configPath); err != nil {
		fmt.Println("Error removing config file:", err)
		return err
	}

	// 获取内网IP地址
	ip, err := sys.GetLocalIP()
	if err != nil {
		fmt.Println("Error retrieving the local IP address:", err)
		return err
	}

	// 将 <YOUR_SERVER_DIR> 替换成实际的内网IP地址
	configData := strings.Replace(template.ConfigTemplate, "<YOUR_SERVER_DIR>", ip, -1)

	// 创建一个新的配置文件模板 写到配置
	if err := os.WriteFile(configPath, []byte(configData), 0644); err != nil {
		fmt.Println("Error writing config.yml:", err)
		return err
	}

	sys.RestartApplication()

	return nil
}

// 获取ws地址数组
func GetWsAddress() []string {
	mu.RLock()
	defer mu.RUnlock()
	if instance != nil {
		return instance.Settings.WsAddress
	}
	return nil // 返回nil，如果instance为nil
}

// 获取WsToken
func GetWsToken() []string {
	mu.RLock()
	defer mu.RUnlock()
	if instance != nil {
		return instance.Settings.WsToken
	}
	return nil // 返回nil，如果instance为nil
}

// 获取DisableErrorChan的值
func GetDisableErrorChan() bool {
	mu.RLock()
	defer mu.RUnlock()

	if instance == nil {
		fmt.Println("Warning: instance is nil when trying to DisableErrorChan value.")
		return false
	}
	return instance.Settings.DisableErrorChan
}

// 获取GetReconnecTimes的值
func GetReconnecTimes() int {
	mu.RLock()
	defer mu.RUnlock()

	if instance == nil {
		fmt.Println("Warning: instance is nil when trying to ReconnecTimes value.")
		return 50
	}
	return instance.Settings.ReconnecTimes
}

// 获取GetHeartBeatInterval的值
func GetHeartBeatInterval() int {
	mu.RLock()
	defer mu.RUnlock()

	if instance == nil {
		fmt.Println("Warning: instance is nil when trying to HeartBeatInterval value.")
		return 5
	}
	return instance.Settings.HeartBeatInterval
}

// 获取LaunchReconectTimes
func GetLaunchReconectTimes() int {
	mu.RLock()
	defer mu.RUnlock()

	if instance == nil {
		fmt.Println("Warning: instance is nil when trying to LaunchReconectTimes value.")
		return 3
	}
	return instance.Settings.LaunchReconectTimes
}

// 获取Uin int64
func GetUinint64() int64 {
	mu.RLock()
	defer mu.RUnlock()
	if instance != nil {
		return instance.Settings.Uin
	}
	return 0
}

// 获取Array的值
func GetArrayValue() bool {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		mylog.Println("Warning: instance is nil when trying to get array value.")
		return false
	}
	return instance.Settings.Array
}

// 获取GetTransferUrl的值
func GetNativeOb11() bool {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		mylog.Println("Warning: instance is nil when trying to NativeOb11 value.")
		return false
	}
	return instance.Settings.NativeOb11
}

// 获取StringOb11的值
func GetStringOb11() bool {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		fmt.Println("Warning: instance is nil when trying to StringOb11 value.")
		return false
	}
	return instance.Settings.StringOb11
}

// GetTimeOut
func GetTimeOut() int {
	mu.Lock()
	defer mu.Unlock()

	if instance == nil {
		mylog.Println("Warning: instance is nil when trying to TimeOut value.")
		return 200
	}
	return instance.Settings.TimeOut
}

package multid

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// 配置项结构体
type IdMap struct {
	OriginID      string   `json:"origin_id"`
	AlternativeID []string `json:"alternative_ids"`
	Names         []string `json:"names"`
	ActiveID      string   `json:"active_id"`
}

var (
	idMapData          sync.Map          // 使用 sync.Map 来代替普通的 map
	activeIDToOriginID sync.Map          // 使用 sync.Map 来代替普通的 map
	filePath           string            // 配置文件路径
	fileLock           sync.Mutex        // 文件写操作的锁
	watcher            *fsnotify.Watcher // fsnotify 实例
)

// 初始化包，自动加载配置文件并设置热重载
func Init() error {
	// 获取当前执行文件的路径
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	// 获取 idmap.json 文件的路径
	filePath = filepath.Join(filepath.Dir(exePath), "idmap.json")

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// 如果文件不存在，创建一个初始版本的 JSON 文件
		initialData := []IdMap{
			{
				OriginID:      "origin1",
				AlternativeID: []string{"alt1_1", "alt1_2"},
				Names:         []string{"name1", "name2"},
				ActiveID:      "active1",
			},
			{
				OriginID:      "origin2",
				AlternativeID: []string{"alt2_1", "alt2_2"},
				Names:         []string{"name3", "name4"},
				ActiveID:      "active2",
			},
			{
				OriginID:      "origin3",
				AlternativeID: []string{"alt3_1", "alt3_2"},
				Names:         []string{"name5", "name6"},
				ActiveID:      "active3",
			},
		}

		// 将初始数据序列化为 JSON 格式
		fileContent, err := json.MarshalIndent(initialData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal initial data: %v", err)
		}

		// 将数据写入文件
		if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
			return fmt.Errorf("failed to write initial data to file: %v", err)
		}
		fmt.Println("Initial idmap.json created with default data.")
	}

	// 读取初始配置文件
	if err := loadConfig(); err != nil {
		return err
	}

	// 设置热重载
	go watchFileForChanges(filePath)

	return nil
}

// 加载配置文件到内存，并构建倒排索引
func loadConfig() error {
	// 获取当前执行文件的路径
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	// 获取 idmap.json 文件的路径
	filePath = filepath.Join(filepath.Dir(exePath), "idmap.json")

	// 读取 JSON 文件
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// 输出读取的文件内容以帮助调试
	//fmt.Printf("File content read from '%s':\n%s\n", filePath, string(fileContent))

	// 解析 JSON 数据
	var idMaps []IdMap
	if err := json.Unmarshal(fileContent, &idMaps); err != nil {
		// 输出 JSON 解析错误
		return fmt.Errorf("failed to parse JSON: %v\nFile content: %s", err, string(fileContent))
	}

	// 清空现有的 map 数据
	idMapData = sync.Map{}
	activeIDToOriginID = sync.Map{}

	// 将数据加载到 idMapData 中，并构建倒排索引
	for _, idMap := range idMaps {
		idMapData.Store(idMap.OriginID, &idMap)
		if idMap.ActiveID != "" {
			activeIDToOriginID.Store(idMap.ActiveID, idMap.OriginID)
		}
	}

	return nil
}

// 监听文件变更并热更新配置
func watchFileForChanges(configPath string) {
	// 创建一个新的 fsnotify watcher 实例
	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("Error creating file watcher: %v\n", err)
		return
	}
	defer watcher.Close()

	// 添加需要监听的文件路径
	err = watcher.Add(configPath)
	if err != nil {
		fmt.Printf("Error adding file to watcher: %v\n", err)
		return
	}

	// 开始监听文件变化
	for {
		select {
		case event := <-watcher.Events:
			// 如果文件发生变动，重新加载配置
			if event.Op&fsnotify.Write == fsnotify.Write {
				// 重新加载配置
				fmt.Println("File modified, reloading config...")
				// 尝试重新加载配置
				if err := loadConfig(); err != nil {
					fmt.Printf("Error reloading config: %v\n", err)
				}

			}
		case err := <-watcher.Errors:
			fmt.Printf("Error watching file: %v\n", err)
		}
	}
}

// 根据 origin_id 获取当前生效的 id
func GetActiveID(originID string) string {
	// 查找指定 origin_id 的配置项
	if idMap, found := idMapData.Load(originID); found {
		if idMap != nil {
			return idMap.(*IdMap).ActiveID
		}
	}
	// 如果没有找到或 ActiveID 为空，返回 origin_id
	return originID
}

// 根据 origin_id 获取完整的 IdMap 结构体
func GetIdMap(originID string) (*IdMap, error) {
	// 查找指定 origin_id 的配置项
	idMap, found := idMapData.Load(originID)
	if !found {
		return nil, errors.New("origin_id not found")
	}
	return idMap.(*IdMap), nil
}

// 根据 origin_id 设置 IdMap 并更新到文件
func SetIdMap(originID string, newIdMap *IdMap) error {
	// 更新内存中的数据
	idMapData.Store(originID, newIdMap)

	// 将更新后的数据写回到文件
	if err := saveConfig(); err != nil {
		// 在写文件失败时回滚内存中的更改
		idMapData.Delete(originID)
		return err
	}

	return nil
}

// 将内存中的数据保存到文件
func saveConfig() error {
	// 锁住文件操作
	fileLock.Lock()
	defer fileLock.Unlock()

	// 将 idMapData 转换为切片
	var idMaps []IdMap
	idMapData.Range(func(key, value interface{}) bool {
		idMaps = append(idMaps, *value.(*IdMap))
		return true
	})

	// 转换为 JSON 格式
	fileContent, err := json.MarshalIndent(idMaps, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	// 写入文件
	if err := os.WriteFile(filePath, fileContent, 0644); err != nil {
		return fmt.Errorf("failed to write file: %v", err)
	}

	return nil
}

// 根据 ActiveID 追溯到对应的 origin_id
// 如果没有找到，返回传入的 activeID
func GetOriginIDFromActiveID(activeID string) string {
	// 通过倒排索引查找 originID
	originID, found := activeIDToOriginID.Load(activeID)
	if !found {
		// 如果找不到对应的 originID，直接返回原始的 activeID
		return activeID
	}
	return originID.(string)
}

// gensokyo-mcp/main.go
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/hoshinonyaruko/gensokyo-mcp/Processor"
	"github.com/hoshinonyaruko/gensokyo-mcp/botstats"
	"github.com/hoshinonyaruko/gensokyo-mcp/callapi"
	"github.com/hoshinonyaruko/gensokyo-mcp/config"
	"github.com/hoshinonyaruko/gensokyo-mcp/praser"
	"github.com/hoshinonyaruko/gensokyo-mcp/sys"
	"github.com/hoshinonyaruko/gensokyo-mcp/template"
	"github.com/hoshinonyaruko/gensokyo-mcp/wsclient"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gopkg.in/fsnotify.v1"
)

// 消息处理器，持有 openapi 对象
var wsClients []*wsclient.WebSocketClient

// ---------- Context helpers ----------

type bearerKey struct{}

func withBearer(ctx context.Context, b string) context.Context {
	return context.WithValue(ctx, bearerKey{}, b)
}

func bearerFromRequest(_ context.Context, r *http.Request) context.Context {
	fmt.Printf("headers: %+v\n", r.Header) // 打印所有header
	return withBearer(r.Context(), r.Header.Get("Authorization"))
}

func bearerFromEnv(ctx context.Context) context.Context {
	return withBearer(ctx, os.Getenv("BEARER"))
}

// ---------- WebSocket tool handler ----------

// ---------- MCP server wrapper ----------

type GensokyoServer struct {
	srv *server.MCPServer
}

func NewGensokyoServer() *GensokyoServer {
	s := server.NewMCPServer(
		"gensokyo-mcp",
		"0.1.0",
		server.WithResourceCapabilities(true, true),
		server.WithToolCapabilities(true),
	)

	wsTool := mcp.NewTool("call_ws",
		mcp.WithDescription("连接目标 Onebot Ws 调用bot并取得回复."),
		mcp.WithString("payload",
			mcp.Description("可选：发送到服务器的文本负载"),
			mcp.DefaultString("帮助"),
		),
		mcp.WithString("user_id",
			mcp.Description("可选：测试使用的user_id"),
			mcp.DefaultString("0"),
		),
		mcp.WithString("group_id",
			mcp.Description("可选：测试使用的group_id"),
			mcp.DefaultString("0"),
		),
		mcp.WithNumber("timeout",
			mcp.Description("连接与首条消息读取超时，单位秒，默认 10"),
			mcp.DefaultNumber(10),
			mcp.Min(1),
		),
	)

	// 可以add 多个tool
	s.AddTool(wsTool, callWS)
	return &GensokyoServer{srv: s}
}

func (g *GensokyoServer) HTTPServer() *server.StreamableHTTPServer {
	return server.NewStreamableHTTPServer(
		g.srv,
		server.WithHTTPContextFunc(bearerFromRequest), // 把 Authorization 注入 ctx
	)
}

func (g *GensokyoServer) ServeStdio() error {
	return server.ServeStdio(
		g.srv,
		server.WithStdioContextFunc(bearerFromEnv), // 从环境变量注入 ctx
	)
}

// ---------- main ------------

func main() {
	transport := flag.String("t", "http", "Transport: http | stdio")
	addr := flag.String("addr", ":8090", "HTTP listen address")
	flag.Parse()

	s := NewGensokyoServer()

	if _, err := os.Stat("config.yml"); os.IsNotExist(err) {
		var err error

		// 将 <YOUR_SERVER_DIR> 替换成实际的内网IP地址 确保初始状态webui能够被访问
		configData := template.ConfigTemplate

		// 将修改后的配置写入 config.yml
		err = os.WriteFile("config.yml", []byte(configData), 0644)
		if err != nil {
			log.Println("Error writing config.yml:", err)
			return
		}

		log.Println("请配置config.yml然后再次运行.")
		log.Print("按下 Enter 继续...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(0)
	}

	// 主逻辑
	// 加载配置
	conf, err := config.LoadConfig("config.yml", false)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	// 配置热重载
	go setupConfigWatcher("config.yml")

	//创建botstats数据库
	botstats.InitializeDB()

	sys.SetTitle(conf.Settings.Title)

	// 启动多个WebSocket客户端的逻辑
	if !allEmpty(conf.Settings.WsAddress) {
		wsClientChan := make(chan *wsclient.WebSocketClient, len(conf.Settings.WsAddress))
		errorChan := make(chan error, len(conf.Settings.WsAddress))
		// 定义计数器跟踪尝试建立的连接数
		attemptedConnections := 0
		for _, wsAddr := range conf.Settings.WsAddress {
			if wsAddr == "" {
				continue // Skip empty addresses
			}
			attemptedConnections++ // 增加尝试连接的计数
			go func(address string) {
				retry := config.GetLaunchReconectTimes()
				BotID := uint64(config.GetUinint64())
				wsClient, err := wsclient.NewWebSocketClient(address, BotID, retry)
				if err != nil {
					log.Printf("Error creating WebSocketClient for address(连接到反向ws失败) %s: %v\n", address, err)
					errorChan <- err
					return
				}
				wsClientChan <- wsClient
			}(wsAddr)
		}
		// 获取连接成功后的wsClient
		for i := 0; i < attemptedConnections; i++ {
			select {
			case wsClient := <-wsClientChan:
				wsClients = append(wsClients, wsClient)
			case err := <-errorChan:
				log.Printf("Error encountered while initializing WebSocketClient: %v\n", err)
			}
		}

		// 确保所有尝试建立的连接都有对应的wsClient
		if len(wsClients) == 0 {
			log.Println("Error: Not all wsClients are initialized!(反向ws未设置或全部连接失败)")
			// 处理连接失败的情况 只启动正向
			//p = Processor.NewProcessorV2(&conf.Settings)
		} else {
			log.Println("All wsClients are successfully initialized.")
			// 所有客户端都成功初始化
			//p = Processor.NewProcessor(&conf.Settings, wsClients)
		}
	} else {
		// p一定需要初始化
		//p = Processor.NewProcessorV2(&conf.Settings)
		// 如果只启动了http api
		if !conf.Settings.EnableWsServer {
			if conf.Settings.HttpAddress != "" {
				// 对全局生效
				conf.Settings.HttpOnlyBot = true
				log.Println("提示,目前只启动了httpapi,正反向ws均未配置.")
			} else {
				log.Println("提示,目前你配置了个寂寞,httpapi没设置,正反ws都没配置.")
			}
		} else {
			if conf.Settings.HttpAddress != "" {
				log.Println("提示,目前启动了正向ws和httpapi,未连接反向ws")
			} else {
				log.Println("提示,目前启动了正向ws,未连接反向ws,httpapi未开启")
			}
		}
	}

	switch *transport {
	case "stdio":
		if err := s.ServeStdio(); err != nil {
			log.Fatalf("stdio server: %v", err)
		}
	case "http":
		serveHTTP(context.TODO(), s.srv, *addr)
	default:
		log.Fatalf("unknown transport: %s", *transport)
	}

}

// ---------- 启动 HTTP 服务器：/mcp → Streamable HTTP  /sse → 旧式 SSE ----------
func serveHTTP(ctx context.Context, core *server.MCPServer, addr string) error {
	streamSrv := server.NewStreamableHTTPServer(
		core,
		server.WithHTTPContextFunc(bearerFromRequest), // 注入 bearer
	)

	sseSrv := server.NewSSEServer(
		core,
		server.WithStaticBasePath("/sse"), // 旧客户端连 GET /sse 拿 schema
		server.WithBaseURL("http://127.0.0.1"+addr),  // 生成绝对路径
		server.WithSSEContextFunc(bearerFromRequest), // 同样注入 bearer
	)

	mux := http.NewServeMux()
	mux.Handle("/mcp", streamSrv) // 单端点即可完成初始化 + 调用 + 流
	mux.Handle("/sse/", sseSrv)   // /sse (GET) 取 schema, /sse/message (POST) 发消息
	mux.Handle("/sse", http.RedirectHandler("/sse/", http.StatusMovedPermanently))

	httpSrv := &http.Server{Addr: addr, Handler: mux}

	// 异步启动
	errCh := make(chan error, 1)
	go func() {
		log.Printf("🚀 Streamable HTTP → http://127.0.0.1%[1]s/mcp\n", addr)
		log.Printf("🚀 SSE            → http://127.0.0.1%[1]s/sse\n", addr)
		errCh <- httpSrv.ListenAndServe()
	}()

	// 监听系统信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("🛑 got %v, shutting down...", sig)
		_ = httpSrv.Close()
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}

func setupConfigWatcher(configFilePath string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Error setting up watcher: %v", err)
	}

	// 添加一个100毫秒的Debouncing
	//fileLoader := &config.ConfigFileLoader{EventDelay: 100 * time.Millisecond}

	// Start the goroutine to handle file system events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return // Exit if channel is closed.
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					fmt.Println("检测到配置文件变动:", event.Name)
					//fileLoader.LoadConfigF(configFilePath)
					config.LoadConfig(configFilePath, true)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return // Exit if channel is closed.
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	// Add the config file to the list of watched files.
	err = watcher.Add(configFilePath)
	if err != nil {
		log.Fatalf("Error adding watcher: %v", err)
	}
}

// allEmpty checks if all the strings in the slice are empty.
func allEmpty(addresses []string) bool {
	for _, addr := range addresses {
		if addr != "" {
			return false
		}
	}
	return true
}

// callWS 连接指定 WebSocket，写入 payload（若有），
// 读取首条文本消息并作为工具结果返回。
// 若首条消息内容为 "帮助"，则通过 ProcessGroupMessage 转发到群聊。
func callWS(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// ---------- 1. 解析参数 ----------
	var args struct {
		Payload string `json:"payload"`
		UserID  string `json:"user_id"`
		GroupID string `json:"group_id"`
		Timeout int    `json:"timeout"`
	}
	if err := req.BindArguments(&args); err != nil {
		return mcp.NewToolResultErrorFromErr("参数解析失败", err), err
	}

	if args.Payload == "" {
		args.Payload = "帮助"
	}

	fmt.Printf("receive:%s \n", args.Payload)

	PrintCallToolRequestAsJSON(req)

	// ---------- 3. 业务逻辑 ----------
	// 异步发送群聊消息；bearer 已确保有值
	go Processor.ProcessGroupMessage(req, wsClients)

	var message *callapi.ActionMessage
	var err error
	var resp string
	// 首先获取超时时间和长查询命令列表
	timeout := config.GetTimeOut()

	// 发送消息给WS接口，并等待响应
	message, err = wsclient.WaitForActionMessage(args.UserID, time.Duration(timeout)*time.Second) // 使用新的超时时间
	if err != nil {
		log.Printf("Error waiting for action message: %v", err)
		resp = "等待超时"
		return mcp.NewToolResultText(resp), nil
	}

	var strmessage string
	// 尝试将message.Params.Message断言为string类型
	if msgStr, ok := message.Params.Message.(string); ok {
		strmessage = msgStr
	} else {
		// 如果不是string，调用parseMessage函数处理
		strmessage = praser.ParseMessageContent(message.Params.Message, false)
	}

	// 调用信息处理函数
	messageType, resultText, resultImg, err := ProcessMessage(strmessage, message)
	if err != nil {
		// 处理错误情况
		resp = "处理错误"
		return mcp.NewToolResultText(resp), nil
	}

	// 根据信息处理函数的返回类型决定如何回复
	switch messageType {
	case 1: // 纯文本信息
		var pendingMsgsToReturn []callapi.ActionMessage
		if resultStr, ok := resultText.(string); ok {
			// 获取并叠加历史信息，传入当前字数（这里假设当前字数为0）
			pendingMsgsToReturn, _, err = wsclient.GetPendingMessages(args.UserID, true, len(resultStr))
			if err != nil {
				log.Printf("Error getting pending messages: %v", err)
				// 如果无法获取历史消息，就直接处理当前的消息
				pendingMsgsToReturn = nil
			}
		}

		// 遍历所有历史消息，并叠加到 result 前
		for _, message := range pendingMsgsToReturn {
			var historyContent string
			// 处理历史消息内容
			if msgStr, ok := message.Params.Message.(string); ok {
				historyContent = msgStr
			} else {
				// 如果不是string类型，调用parseMessage函数处理
				historyContent = praser.ParseMessageContent(message.Params.Message, true)
			}

			// 将历史信息叠加到当前的 result 前
			resultText = fmt.Sprintf("%s\n-----历史信息----\n%s", historyContent, resultText)
		}
		return mcp.NewToolResultText(resultText.(string)), nil
	case 2: // 纯图片信息
		imgBase64, err := ImageURLToBase64(resultImg.(string))
		if err != nil {
			return nil, err
		}
		return mcp.NewToolResultImage(resultText.(string), imgBase64, "image/jpeg"), nil

	case 4: // 图文信息
		imgBase64, err := ImageURLToBase64(resultImg.(string))
		if err != nil {
			return nil, err
		}
		return mcp.NewToolResultImage(resultText.(string), imgBase64, "image/jpeg"), nil
	// 	case 2: // 纯图片信息
	// 	return NewToolResultTwoTexts(resultText.(string), resultImg.(string)), nil
	// case 4: // 图文信息
	// 	return NewToolResultTwoTexts(resultText.(string), resultImg.(string)), nil
	default:
		return mcp.NewToolResultText("未知类型信息"), nil
	}
	//return mcp.NewToolResultText("无返回值"), nil
}

// ProcessMessage 处理信息并归类
func ProcessMessage(input string, rawMsg *callapi.ActionMessage) (int, interface{}, interface{}, error) {
	// 正则表达式定义
	httpUrlImagePattern := regexp.MustCompile(`\[CQ:image,file=http://(.+?)\]`)
	httpsUrlImagePattern := regexp.MustCompile(`\[CQ:image,file=https://(.+?)\]`)
	base64ImagePattern := regexp.MustCompile(`\[CQ:image,file=base64://(.+?)\]`)
	base64RecordPattern := regexp.MustCompile(`\[CQ:record,file=base64://(.+?)\]`)
	httpUrlRecordPattern := regexp.MustCompile(`\[CQ:record,file=http://(.+?)\]`)
	httpsUrlRecordPattern := regexp.MustCompile(`\[CQ:record,file=https://(.+?)\]`)

	// 检查是否含有base64编码的图片或语音信息
	var err error
	if base64ImagePattern.MatchString(input) || base64RecordPattern.MatchString(input) {
		input, err = processInput(input)
		if err != nil {
			log.Printf("processInput出错:\n%v\n", err)
		}
		log.Printf("处理后的base64编码的图片或语音信息:\n%v\n", input)
	}

	// 检查是否为纯文本信息
	if !httpUrlImagePattern.MatchString(input) && !httpsUrlImagePattern.MatchString(input) && !httpUrlRecordPattern.MatchString(input) && !httpsUrlRecordPattern.MatchString(input) {
		// 使用正则表达式匹配并替换[CQ:at,qq=x]格式的信息
		cqAtPattern := regexp.MustCompile(`\[CQ:at,qq=\d+\]`)
		// 将匹配到的部分替换为空字符串
		filteredInput := cqAtPattern.ReplaceAllString(input, "")

		// 返回过滤后的纯文本信息
		return 1, filteredInput, nil, nil
	}

	// 图片信息处理
	if httpUrlImagePattern.MatchString(input) || httpsUrlImagePattern.MatchString(input) {
		// 合并匹配到的所有图片URL
		httpImageUrls := httpUrlImagePattern.FindAllStringSubmatch(input, -1)
		httpsImageUrls := httpsUrlImagePattern.FindAllStringSubmatch(input, -1)

		// 通过前缀重新构造完整的图片URL
		var imageUrls []string
		for _, match := range httpImageUrls {
			imageUrls = append(imageUrls, "http://"+match[1])
		}
		for _, match := range httpsImageUrls {
			imageUrls = append(imageUrls, "https://"+match[1])
		}

		// 替换掉所有图片标签
		input = httpUrlImagePattern.ReplaceAllString(input, "")
		input = httpsUrlImagePattern.ReplaceAllString(input, "")

		// 如果替换后内容为空 且只有一个图片
		if len(imageUrls) == 1 && input == "" {
			imgUrl := imageUrls[0]
			return 2, "", imgUrl, nil // 纯图片信息
		} else {
			// 图片信息+文本
			imgUrl := imageUrls[0]

			// 将文字部分加入到堆积的事件中
			//wsclient.AddMessageToPending(rawMsg.Params.UserID.(string), rawMsg)
			return 2, input, imgUrl, nil // 图文信息
		}
	}

	// 语音信息处理
	if httpUrlRecordPattern.MatchString(input) || httpsUrlRecordPattern.MatchString(input) {
		// 初始化变量用于存放处理后的URL
		var recordUrl string

		// 查找匹配的HTTP URL
		httpRecordMatches := httpUrlRecordPattern.FindAllStringSubmatch(input, -1)
		if len(httpRecordMatches) > 0 {
			// 取第一个匹配项，并添加HTTP前缀
			recordUrl = "http://" + httpRecordMatches[0][1]
		}

		// 查找匹配的HTTPS URL
		httpsRecordMatches := httpsUrlRecordPattern.FindAllStringSubmatch(input, -1)
		if len(httpsRecordMatches) > 0 {
			// 如果已经找到HTTP URL，优先处理HTTPS URL
			recordUrl = "https://" + httpsRecordMatches[0][1]
		}

		// 如果找到了语音URL
		if recordUrl != "" {
			mediaId := 0
			return 3, mediaId, nil, nil // 纯语音信息
		}
	}

	// 如果没有匹配到任何已知格式，返回错误
	return 0, nil, nil, errors.New("unknown message format")
}

// processInput 处理含有Base64编码的图片和语音信息的字符串
func processInput(input string) (string, error) {

	return input, nil
}

// 转为json并打印
func PrintCallToolRequestAsJSON(req mcp.CallToolRequest) error {
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// NewToolResultTwoTexts creates a new CallToolResult with two text content elements
func NewToolResultTwoTexts(text1, text2 string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: text1,
			},
			mcp.TextContent{
				Type: "text",
				Text: text2,
			},
		},
	}
}

// ImageURLToBase64 downloads an image from a URL and returns its base64 encoding as a string
func ImageURLToBase64(url string) (string, error) {
	// 1. 发起 GET 请求
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 2. 读取所有内容到内存
	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 3. base64 编码
	base64Str := base64.StdEncoding.EncodeToString(imgData)
	return base64Str, nil
}

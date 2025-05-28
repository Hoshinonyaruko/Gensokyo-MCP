package structs

type FriendData struct {
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
	UserID   string `json:"user_id"`
}

type Settings struct {
	//反向ws设置
	WsAddress           []string `yaml:"ws_address"`
	WsToken             []string `yaml:"ws_token"`
	ReconnecTimes       int      `yaml:"reconnect_times"`
	HeartBeatInterval   int      `yaml:"heart_beat_interval"`
	LaunchReconectTimes int      `yaml:"launch_reconnect_times"`
	//基础配置
	Uin              int64  `yaml:"uin"`
	DisableErrorChan bool   `yaml:"disable_error_chan"`
	Title            string `yaml:"title"`
	EnableWsServer   bool   `yaml:"enable_ws_server"`
	HttpAddress      string `yaml:"http_address"`
	HttpOnlyBot      bool   `yaml:"http_only_bot"`
	Array            bool   `yaml:"array"`
	NativeOb11       bool   `yaml:"native_ob11"`
	StringOb11       bool   `yaml:"string_ob11"`
	TimeOut          int    `yaml:"timeOut"`
}

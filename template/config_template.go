package template

const ConfigTemplate = `
version: 1
settings:
  #反向ws设置
  ws_address: ["ws://<YOUR_WS_ADDRESS>:<YOUR_WS_PORT>"] # WebSocket服务的地址 支持多个["","",""]
  ws_token: ["","",""]              #连接wss地址时服务器所需的token,按顺序一一对应,如果是ws地址,没有密钥,请留空.
  reconnect_times : 100             #反向ws连接失败后的重试次数,希望一直重试,可设置9999
  heart_beat_interval : 5          #反向ws心跳间隔 单位秒 推荐5-10
  launch_reconnect_times : 1        #启动时尝试反向ws连接次数,建议先打开应用端再开启gensokyo,因为启动时连接会阻塞webui启动,默认只连接一次,可自行增大

  #基础设置
  uin : 0                                            # 你的机器人QQ号
  timeOut : 4                                          # 等待反向ws信息超时时间,默认4秒,当超时时,可以触发默认回复,引导用户。
  disable_error_chan : false        #禁用ws断开时候将信息放入补发频道,当信息非常多时可能导致冲垮应用端,可以设置本选项为true.
  string_ob11 : false               #api不再返回转换后的int类型,而是直接转换,需应用端适配.
  string_action : false             #开启后将兼容action调用中使用string形式的user_id和group_id.
  title : "gensokyo-mcp © 2025 - Hoshinonyaruko"              #程序的标题 如果多个机器人 可根据标题区分
`
const Logo = `
'                                                                                                      
'    ,hakurei,                                                      ka                                  
'   ho"'     iki                                                    gu                                  
'  ra'                                                              ya                                  
'  is              ,kochiya,    ,sanae,    ,Remilia,   ,Scarlet,    fl   and  yu        ya   ,Flandre,   
'  an      Reimu  'Dai   sei  yas     aka  Rei    sen  Ten     shi  re  sca    yu      ku'  ta"     "ko  
'  Jun        ko  Kirisame""  ka       na    Izayoi,   sa       ig  Koishi       ko   mo'   ta       ga  
'   you.     rei  sui   riya  ko       hi  Ina    baI  'ran   you   ka  rlet      komei'    "ra,   ,sa"  
'     "Marisa"      Suwako    ji       na   "Sakuya"'   "Cirno"'    bu     sen     yu''        Satori  
'                                                                                ka'                   
'                                                                               ri'                    
`

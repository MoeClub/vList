# 189List
#### A list for `https://cloud.189.cn/`
#### 天翼网盘列表程序
- 支持添加多账户
- 支持异步缓存目录结构, 无须等待
- 支持整个目录, 单层目录或单文件访问加密
- 支持展示任意目录, 自定义根目录
- 支持每天自动签到, 获取额外空间奖励
- 支持内容以 JSON 格式输出, 方便作为后端.
- 支持只读挂载到 `PotPlayer(WebDAV)`, `nPlayer(WebDAV)`, `kodi(WebDAV)`

## 配置文件 `config.json`
```
# 单用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/"
  }
]

# 多用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/189Cloud01"
  },
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/189Cloud02"
  }
]

```


## 目录样式文件 [index.html](https://raw.githubusercontent.com/MoeClub/vList/master/index.html)
##### 可以自定义呈现样式, 编辑单文件 index.html 即可.
```
https://raw.githubusercontent.com/MoeClub/vList/master/index.html

```

## 运行
```
# 直接运行
./189List -bind 0.0.0.0 -port 80

# 后台运行
./189List -bind 0.0.0.0 -port 80 -d

```

# 配置文件项解释
#### 注意: `json`文件格式标准中规定最后一行数据没有逗号.
```
[
  {
    "disable": false,
    // 是否关闭该配置
    
    "user": "手机号",
    
    "passwd": "密码",
    
    "rootId": "-11",
    // 根目录文件夹ID, 默认 -11
    
    "rootPath": "/Cloud189",
    // 挂载的虚拟路径
    
    "authItem": "abc:123@/Movie|xyz:456@/Private/*",
    // HTTP 401 加密项, 以 | 为分隔符. 可以 * 结尾匹配路径. 路径为去掉挂载虚拟路径后的路径.
    // abc:123@/Movie  当访问 /Cloud189/Movie 时需要提供用户名 abc 和 密码 123, 但 /Cloud189/Movie 下的子项无需提供用户名密码.
    // xyz:456@/Private/*   当访问 /Cloud189/Private 及其子项时需要提供用户名 xyz 和 密码 456.
    
    "nodeInterval": 1800,
    // 目录刷新间隔, 最小值: 300
    
    "linkInterval": 300
    // 下载链接刷新间隔, 最小值: 60, 最大值 360
    
  }
]
```

# 启动参数项
```
Usage of ./189List:
  -bind string
        Bind Address (default "127.0.0.1")
  -port string
        Port (default "5189")

  -c string
        Config File. (default "config.json")
  -i string
        Index File. (default "index.html")

  -sttl int
        Session Time To Live. (default 14400)
  -nttl int
        Folder Time To Live. (default 1800)
  -ttl int
        Download Link Time To Live. (default 300)

  -ua string
        Set UserAgent. (default "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:91.0) Gecko/20100101 Firefox/91.0")

  -webdav
        WebDAV Support. (default true)
  -j    JSON Format Output.

  -d    Daemon (Linux Only).
  -q    Quiet.
        
```

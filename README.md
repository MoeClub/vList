# 特征简介
```
1. 用户名密码鉴权
2. 隐藏/忽略 某文件
3. 隐藏/忽略 某文件夹
4. 正则匹配支持
5. 302 链接 映射成文件
6. 文件断点续传
7. 读取配置文件
8. WebDVA 只读模式
9. index.html
```

# 配置文件 (config.json)
```
{
  "WorkFolder": "/var/www",
  # 展示某个目录(唯一)
  # 示例: /var/www.
  
  "Endpoint": "/",
  # 映射到URL目录, 默认为根目录.
  
  "FolderSize": false,
  # 计算文件夹大小, 递归遍历累加. (开启会影响性能)
  
  "AuthItem": "user1:passwd1@/The/File/Path|user2:passwd2@/The/Folder/Path",
  # 使用 HTTP 401 加密多个目录或者文件.
  # 路径格式为省略掉 Endpoint 的相对路径.
  
  "RedirectItem" : "google.txt;https://google.com|/link/baidu.txt;https://baiud.com",
  # 添加虚拟路径, 302 跳转至指定链接.
  # 路径格式为省略掉 Endpoint 的相对路径.
  
  "IgnoreFile": "",
  # 忽略某个名字的文件, 支持正则.
  
  "IgnoreFolder": "",
  # 忽略某个名字的文件夹, 支持正则.
  
  "HideFile": ".*\\.sh|test\\.txt",
  # 隐藏某个名字的文件, 支持正则. 
  # 示例: 隐藏 sh 后缀的文件和名字为 test.txt 的文件.
  
  "HideFolder": "",
  # 隐藏某个名字的文件夹, 支持正则.
  
  "WebDAV": true
  # 添加只读模式的 WebDAV 访问功能.
  
  # 隐藏: 不显示在列表中, 但能可以访问.
  # 忽略: 不显示在列表中, 且不可以访问.
  # 某一项中填写多个项目, 使用 "|" 分隔.
  # 正则表达式转义符在 json 文件中为 "\\"
}

```

# 使用方法

## 快速使用
```
./vList -h
./vList -w "/var/www"
./vList -q -w "/var/www" -bind 0.0.0.0 -port 8080
./vList -bind 0.0.0.0 -port 8080 -w "/var/www" -webdav=false -hide=fasle -size=true

# -h 显示帮助
# -q 后台运行(在Windows下无效)
```

## 应用配置文件
##### 如果同目录中有 config.json, 则会自动读取.
##### 如果同目录中有 index.html, 则会自动读取.
```
./vList
./vList -c "/配置文件绝对路径/config.json" -bind 0.0.0.0 -port 8080
```


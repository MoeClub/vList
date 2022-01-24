# 189List
#### A list for `https://cloud.189.cn/`

## 配置文件 `config.json`
```
# 单用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/",
    "authItem": ""
  }
]

# 多用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/189Cloud01",
    "authItem": ""
  },
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/189Cloud02"
    "authItem": "",
  }
]

```


## 目录样式文件 [index.html](https://raw.githubusercontent.com/MoeClub/vList/master/index.html)
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
    
    "authItem": "abc:123@/Movie|xyz:456@/Private/*"
    // HTTP 401 加密项, 以 | 为分隔符. 可以 * 结尾匹配路径.
    // abc:123@/Movie  当访问 /Cloud189/Movie 时需要提供用户名 abc 和 密码 123, 但 /Cloud189/Movie 下的子项无需提供用户名密码.
    // xyz:456@/Private/*   当访问 /Cloud189/Private 及其子项时需要提供用户名 xyz 和 密码 456.
    
    "nodeInterval": 1800,
    // 目录刷新间隔, 最小值: 300
    
    "linkInterval": 300,
    // 下载链接刷新间隔, 最小值: 60, 最大值 360
    
  }
]

```

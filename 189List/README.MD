# 189List
#### A list for `https://cloud.189.cn/`

## 配置文件 `config.json` [单用户]
```
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11",
    "rootPath": "/"
  }
]

```

## 配置文件 `config.json` [多用户]
```
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

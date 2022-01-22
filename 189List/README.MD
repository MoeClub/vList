# 189List
#### A list for `https://cloud.189.cn/`

## config.json
```
# 单用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11"
  }
]

# 多用户
[
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11"
  },
  {
    "user": "手机号",
    "passwd": "密码",
    "rootId": "-11"
  },
]

```

## 运行
```
# 直接运行
./189List -bind 0.0.0.0 -port 80

# 后台运行
./189List -bind 0.0.0.0 -port 80 -d

```

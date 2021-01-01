# Zhima Proxy Pool

----

**[EN](README_en.md)**

一个为[芝麻HTTP](http://h.zhimaruanjian.com)按次付费设计的Proxy Pool，以经济为目标。

### 实现

Proxy分为两类，Active和Backup。

- ActiveProxy是已付费的proxy，数量不宜过多，否则不经济；也不宜过少，否则容易单个代理请求过快/过多，导致ip被封，即使这个代理在别的地址还能用，但也不得不丢弃，也不经济。
- BackupProxy是已获取而未付费的proxy，可以随意丢弃，BackupProxy不够时会自动从api拉取。

当从ProxyPool中请求获取Proxy时，会从ActiveProxy中均匀地返回一个Proxy。 如果该Proxy已经过期，则会从BackupProxy中选择一个全新的Proxy，放进ActiveProxy中替换掉过期的Proxy。

## 用法

以5~25分钟的IP为例子

``` go
config := &Config{
    ApiAddr:   "you Api addr",    // 在芝麻http上获得的API地址，数据格式为JSON，并且属性中要勾选过期时间
    BackUpCap: 50,                // 建议为 50 ~ 100，或者数倍于ActiveCap
    ActiveCap: 6,                 // 同时激活的proxy数量，应该根据实际情况适当调整
    ClearTime: time.Second * 90,  // 清除BackupProxy的时间间隔，1 ~ 2分钟即可
    TimeLimit: time.Minute * 23,  // TimeLimit应该设置为比最长时间略少一点的时间
}
pool := NewZhimaProxyPool(config, NewNilPersister()） //创建ProxyPool
proxy, err := pool.Get()
if err != nil {
// 查看错误日志
}
// 使用这个proxy 
// ...
// 当这个proxy未过期但是却无法正常使用的时候调用。
// 为了经济，应该多次重试确认后再谨慎删除，删除的proxy无法再次获取。
pool.Delete(proxy)
```

## 配置

- #### 生成api address
  在 [芝麻HTTP](http://h.zhimaruanjian.com/getapi) 上生成api链接，只支持JSON数据格式，并且属性中必须选择过期时间，其他可任意指定。
- #### 如何确定ActiveCap

以B站爬虫为例，当ActiveCap为1时，表示同时使用单个IP，当单个IP请求过快时，B站会把这个IP封禁一段时间，时间内所有请求返回错误-412。

所以此时应该把ActiveCap适当调大，例如设置为6， 表示最多同时激活6个IP，这样把请求分散在6个IP上，再次观察是否会被封禁（这取决于你的爬虫速度）， 
如果追求极致速度，应当继续调大，直到请求不再或很少返回错误-412。

初次设定时建议使用5~25分钟的IP测试。

- #### 如何确定TimeLimit

推荐设置为略小于你使用的IP时间段的最大值，例如：

|  IP过期时间   | 推荐值  |
|  ----  | ----  |
| 5分钟~25分钟  | 22分钟 |
| 25分钟~3小时  | 2小时57分钟 |
- #### 设置Persister
Persister的目的是为了防止程序重启时丢失已激活的ActiveProxy，所以在程序退出时需要使用Persister存放ActiveProxy。
如果选择NilPersister，则程序每次启动时都会重新激活proxy，这不经济。

默认实现了一个FilePersister，把ActiveProxy存在一个文件中，你也可以自己实现Persister，例如存储在数据库中。

## TODO
- [ ] Proxy用量监控
- [ ] 自动推测ActiveCap

## 声明
本项目尚未处于stable状态， 仅在5分钟\~25分钟 & 25分钟\~3小时测试， 谨慎使用。

## 贡献
欢迎PR。

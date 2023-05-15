# JsRPC
[![](https://hits.seeyoufarm.com/api/count/incr/badge.svg?url=https%3A%2F%2Fgithub.com%2Fmzzsfy%2FRpcServer&count_bg=%2379C83D&title_bg=%23555555&icon=&icon_color=%23E7E7E7&title=hits&edge_flat=false)](https://github.com/mzzsfy)
## 基本介绍

运行服务器程序和js脚本 即可让它们通信，实现调用接口执行js获取想要的值

## 食用方法

### 打开编译好的文件，开启服务

**api 简介**

- `/list` : 查看当前连接的ws服务
- `/dash` : 一个简单的统计
- `/die` : 一个简单历史连接数据
- `/ws`  : 浏览器注入ws连接的接口
- `/call` : 执行注册的js group={}&name={}&action={}&param={}
- `/exec` : 执行代码 group={}&name={}&code={}

说明：接口用?group和name来区分任务 如 ws://127.0.0.1:18880/ws?group={}&name={}"  
//注入ws的例子 group和name都可以随便起名 name为空则会随机  
http://127.0.0.1:18880/call?group={}&name={}&action={}&param={} //这是调用的接口  
name支持简单模糊匹配,模糊匹配时有简单均衡负载,支持前缀后缀匹配或者任意: xxx* 或*xxx  
group和name填写上面注入时候的，action是注册的方法名,param是可选的参数  
额外说明: randomSuffix参数可以在名称后自动生成后缀

### 安全相关

设置环境变量TOKEN=xxx,则call,exec,ws等接口需要在参数上携带该token 设置环境变量WS_TOKEN=xxx,则ws接口需要在参数上携带该token,覆盖TOKEN环境变量的设置
设置环境变量SELECT_TOKEN=xxx,则list和dash接口需要在参数上携带该token,覆盖TOKEN环境变量的设置

### 支持客户端

请参考client目录  
如: [javaScript](./client/javaScript/README.md)

待适配: ios

### 压测

使用js环境数据,其他环境应该性能更高

cpu i5-8500

- 单线程: 响应1ms内,吞吐量接近2k/s
- 10线程: 响应1ms-6ms,吞吐量约1w/s
- 100线程: 响应1-40ms,吞吐量约2.1w/s
- 1000线程: 响应2-80ms,吞吐量约2.6w/s

### 感谢

本项目灵感来自 https://github.com/jxhczhl/JsRpc 非常感谢

主要解决了原项目中并发问题,提供模糊匹配功能,并且针对高并发解决了一系列优化 https://github.com/jxhczhl/JsRpc/issues/7 (对方把我反馈删了,笑)

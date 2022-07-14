# JsRPC

## 基本介绍

运行服务器程序和js脚本 即可让它们通信，实现调用接口执行js获取想要的值

## 食用方法

### 注入JS，构建通信环境

打开client/javaScript/index.js 复制粘贴到网站控制台(注意：可以在浏览器开启的时候就先注入环境，不然要放开调试断点才能注入)

### 连接通信

```js
// 注入环境后连接通信
var demo = new rpcClient("ws://127.0.0.1:18880/ws?group=test");
```

#### I 远程调用0：

##### 接口传js代码让浏览器执行

浏览器已经连接上通信后 调用execjs接口就行

```js
let jscode = `
console.log("test")
return "执行成功"
`

let url = "http://localhost:18880/exec?group=test&name=*&code="+decodeURIComponent(jscode)
let res = get(url)
consloe.log(res.text)
```

#### Ⅱ 远程调用1： 浏览器预先注册js方法 传递函数名调用

##### 远程调用1：无参获取值

```js

// 注册一个方法 第一个参数hello为方法名，
demo.regAction("hello", function () {
    //这样每次调用就会返回“好困啊+随机整数”
    return "hello"+parseInt(Math.random()*1000);
})
```

    访问接口，获得js端的返回值
    http://localhost:18880/call?group=test&name=*&action=hello

##### 远程调用2：带参获取值

```js
//写一个传入字符串，返回base64值的接口(调用内置函数btoa)
demo.regAction("hello2", function (param) {
    //这样添加了一个param参数，http接口带上它，这里就能获得
    return btoa(param);
})
```
    访问接口，获得js端的返回值
    http://localhost:18880/call?group=test&name=*&action=hello2&param=test

### 压测

cpu i5-8500

- 单线程: 响应1ms内,吞吐量接近2k/s
- 10线程: 响应1ms-6ms,吞吐量约1w/s
- 100线程: 响应1-40ms,吞吐量约2.1w/s
- 1000线程: 响应2-80ms,吞吐量约2.6w/s
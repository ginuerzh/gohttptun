go http tunnel
==============

###此代码已废弃，请使https://github.com/ginuerzh/gost

用go语言实现的基于http协议的tunnel，主要用于绕过http代理。
一般公司都会通过代理服务器连接外网，而代理服务器同时也会限制网络访问。
如果你有一台外网主机(例如VPS)没有被代理服务器封掉，那么你就可以利用此程序来访问受限网站。

基本用法：

默认此程序运行为服务器端, 监听端口为8888.

$ gohttptun -L 8888

使用-c选项则作为客户端运行, -L设置本地监听端口，-S设置服务器地址

$ gohttptun -c -L 8080 -S your.server.com:8888

如果处在http代理的后面，可以通过-P设置代理服务器(客户端，服务端均可设置代理)

$ gohttptun -c -L 8080 -S your.server.com:8888 -P your.proxy.com:8000

-b可以用来设置读取缓存大小，默认为8192 Bytes, 服务器端可以设大一点。

接下来就可以畅通无阻的访问网络了！

# 适用于非阿里云集群的oss-csi插件

项目来源于[阿里云Kubernetes CSI插件](https://github.com/kubernetes-sigs/alibaba-cloud-csi-driver)，原插件功能强大， 支持将云盘、NAS、CPFS、OSS、LVM作为集群PV。
但是，如果用的是自建集群，且不使用[自建集群](https://help.aliyun.com/document_detail/121053.html)的方式接入阿里云，我们无法使用该工具（需要内网）。

**2023.3月，alibaba-cloud-csi-driver发布了全新版本，更新了部署yaml，但是文档没有跟上，所以很坑**

本仓库针对个人需求对源代码进行了删改，完美解决了个人需求：
- 只需要支持amd64处理机
- 只需要支持OSS作为存储卷
- 集群结点不算多

如果你的需求和我一样，那么可以参考本项目，或者跟着本教程直接使用

## 1. 安装步骤
### 1.1 宿主机安装ossfs
“ossfs能让您在Linux系统中，将对象存储OSS的存储空间（Bucket）挂载到本地文件系统中，您能够像操作本地文件一样操作OSS的对象（Object），实现数据的共享。”

在原仓库中，宿主机中的ossfs由容器中nsenter管理安装和更新。但是版本非常混乱，因此综合考虑，自己手动为ossfs安装比较方便。

**必须为每一台node安装**，因此假如结点很多，这个方法对你来说不合适（原脚本中会判断宿主机中的OS类型完成安装，但是我还是把这些逻辑删了，因为总会出现离奇bug）。

安装过程参考[ossfs 快速安装](https://help.aliyun.com/document_detail/153892.htm)，安装包给的不多，个人推荐**源码安装**，因为真的比直接下载二进制包方便（仅对于ossfs，官方安装包有点旧）。
源码安装过程直接看[ossfs Github](https://github.com/aliyun/ossfs)，以Ubuntu为例，步骤示例如下：
```shell
apt-get install automake autotools-dev g++ git libcurl4-gnutls-dev \
                     libfuse-dev libssl-dev libxml2-dev make pkg-config
git clone https://github.com/aliyun/ossfs.git
cd ossfs
./autogen.sh
./configure
make
sudo make install
```

### 1.2 k8s安装依赖
在集群中部署[01-rbac.yaml](deploy%2F01-rbac.yaml)和[02-csi-driver.yaml](deploy%2F02-csi-driver.yaml)，分别用于声明权限和定义插件执行Node Attach的方式。文件均来自原仓库同路径文件，直接apply即可：
```shell
kubectl apply -f ./deploy/01-rbac.yaml
kubectl apply -f ./deploy/02-csi-driver.yaml
```

### 1.3 集群中安装oss插件
去[docker仓库](https://hub.docker.com/repository/docker/wujunyi792/oss-csi-lite-plugin/general)中找到最新版本的镜像，替换[03-csi-plugin.yaml](deploy%2F03-csi-plugin.yaml)中容器csi-plugin的image（前提是你也是amd64，仓库中只提供了amd64版本）。

然后直接部署即可：
```shell
kubectl apply -f ./deploy/03-csi-plugin
```
稍等片刻，容器启动成功，你的集群每个结点都会运行csi-plugin的pod
![img.png](pic%2Fpic1.png)

当然，你也可以自己构建镜像（本仓库只支持amd），步骤如下：
1. 修改[build-amd64-image.sh](build%2Fbuild-amd64-image.sh)中的仓库地址等信息
2. `cd build && sh build-amd64-image.sh`
3. 稍等片刻，镜像就会推送到自己的仓库
4. 修改[03-csi-plugin.yaml](deploy%2F03-csi-plugin.yaml)中的镜像为自己的镜像，然后`kubectl apply -f ./deploy/03-csi-plugin`完成安装

## 2. 测试步骤
oss的csi插件已经部署完成，我们接下来创建pv,pvc和一个nginx来做测试。测试yaml同样来自于原仓库
### 2.1 配置存储桶
这里就不细说了，去阿里云oss开一个bucket，然后申请一对AccessID和AccessKey，填写在[01-pv.yaml](examples%2F01-pv.yaml)中，其中`url`为oss外网Endpoint，可在[访问域名和数据中心
](https://help.aliyun.com/document_detail/31837.html)查看

### 2.2 部署pv pvc
```shell
kubectl apply -f ./examples/01-pv.yaml
kubectl apply -f ./examples/02-pvc.yaml
kubectl apply -f ./examples/03-deploy.yaml
```

随后等待nginx部署成功
![pic2.png](pic%2Fpic2.png)

### 2.3 测试
进去 nginx 的 pod shell，往 `/data` 底下写个文件，再去 oss 里看看有没有出现
```shell
# in pod shell
cd /data
echo hello > 1.txt
```
![pic3.png](pic%2Fpic3.png)
![pic4.png](pic%2Fpic4.png)
成功！

## 3. TODO
- [ ] 原仓库还有个 `csi-provisioner.yaml`，里面有一些 StorageClass 方法，直接跑也是跑不起来的，有空可以support以下
- [ ] 支持以下异构集群
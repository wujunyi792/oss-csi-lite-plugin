FROM centos:7

COPY freezefs.sh /freezefs.sh
RUN curl https://aliyun-alinas-eac.oss-cn-beijing.aliyuncs.com/alinas-efc-1.2-2.x86_64.rpm -o /root/alinas-efc-1.2-2.x86_64.rpm
RUN curl https://aliyun-encryption.oss-cn-beijing.aliyuncs.com/aliyun-alinas-utils-1.1-5.al7.noarch.rpm -o /root/aliyun-alinas-utils-1.1-5.al7.noarch.rpm

RUN mkdir /csi && \
    yum install -y ca-certificates file tzdata nfs-utils xfsprogs e4fsprogs pciutils iputils strace cloud-utils-growpart gdisk nc telnet

COPY . /csi
COPY amd64-entrypoint.sh /entrypoint.sh
COPY nsenter /nsenter
COPY nsenter /usr/bin/nsenter
COPY plugin.csi.alibabacloud.com /bin/plugin.csi.alibabacloud.com

RUN chmod +x /csi/plugin.csi.alibabacloud.com && chmod +x /entrypoint.sh && chmod +x /csi/csiplugin-connector && chmod +x /csi/csiplugin-connector.service && \
     ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && echo 'Asia/Shanghai' > /etc/timezone

ENTRYPOINT ["/entrypoint.sh"]

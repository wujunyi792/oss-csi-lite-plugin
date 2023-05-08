#!/bin/sh

echo "start"

mkdir -p /var/log/alicloud/
mkdir -p /host/etc/kubernetes/volumes/disk/uuid

HOST_CMD="/nsenter --mount=/proc/1/ns/mnt"


${HOST_CMD} ls /etc/os-release
os_release_exist=$?

if [[ "$os_release_exist" = "0" ]]; then
    ${HOST_CMD} cat /etc/os-release
fi

echo "Running oss plugin...."
mkdir -p /var/lib/kubelet/csi-plugins/ossplugin.csi.alibabacloud.com
rm -rf /var/lib/kubelet/plugins/ossplugin.csi.alibabacloud.com/csi.sock


## OSS plugin setup
if [ ! `${HOST_CMD}  which ossfs` ]; then
    echo "ossfs don't exist on host machine, please install first"
    exit 1
fi

## install/update csi connector
updateConnector="true"
systemdDir="/host/usr/lib/systemd/system"

if [ ! -f "/host/etc/csi-tool/csiplugin-connector" ];then
    mkdir -p /host/etc/csi-tool/
    echo "mkdir /etc/csi-tool/ directory..."
else
    oldmd5=`md5sum /host/etc/csi-tool/csiplugin-connector | awk '{print $1}'`
    newmd5=`md5sum /csi/csiplugin-connector | awk '{print $1}'`
    if [ "$oldmd5" = "$newmd5" ]; then
        updateConnector="false"
    else
        rm -rf /host/etc/csi-tool/
        rm -rf /host/etc/csi-tool/connector.sock
        rm -rf /var/log/alicloud/connector.pid
        mkdir -p /host/etc/csi-tool/
    fi
fi
cp /freezefs.sh /host/etc/csi-tool/freezefs.sh
if [ "$updateConnector" = "true" ]; then
    echo "Install csiplugin-connector...."
    cp /csi/csiplugin-connector /host/etc/csi-tool/csiplugin-connector
    chmod 755 /host/etc/csi-tool/csiplugin-connector
fi


# install/update csiplugin connector service
updateConnectorService="true"
if [[ ! -z "${PLUGINS_SOCKETS}" ]];then
    sed -i 's/Restart=always/Restart=on-failure/g' /csi/csiplugin-connector.service
    sed -i '/^\[Service\]/a Environment=\"WATCHDOG_SOCKETS_PATH='"${PLUGINS_SOCKETS}"'\"' /csi/csiplugin-connector.service
    sed -i '/ExecStop=\/bin\/kill -s QUIT $MAINPID/d' /csi/csiplugin-connector.service
    sed -i '/^\[Service\]/a ExecStop=sh -xc "if [ x$MAINPID != x ]; then /bin/kill -s QUIT $MAINPID; fi"' /csi/csiplugin-connector.service
fi
if [ -f "$systemdDir/csiplugin-connector.service" ];then
    echo "Check csiplugin-connector.service...."
    oldmd5=`md5sum $systemdDir/csiplugin-connector.service | awk '{print $1}'`
    newmd5=`md5sum /csi/csiplugin-connector.service | awk '{print $1}'`
    if [ "$oldmd5" = "$newmd5" ]; then
        updateConnectorService="false"
    else
        rm -rf $systemdDir/csiplugin-connector.service
    fi
fi

if [ "$updateConnectorService" = "true" ]; then
    echo "Install csiplugin connector service...."
    cp /csi/csiplugin-connector.service $systemdDir/csiplugin-connector.service
    echo "Starting systemctl daemon-reload."
    for((i=1;i<=10;i++));
    do
        ${HOST_CMD} systemctl daemon-reload
        if [ $? -eq 0 ]; then
            break
        else
            echo "Starting retry again systemctl daemon-reload.retry count:$i"
            sleep 2
        fi
    done
fi

rm -rf /var/log/alicloud/connector.pid
echo "Starting systemctl enable csiplugin-connector.service."
for((i=1;i<=10;i++));
do
    ${HOST_CMD} systemctl enable csiplugin-connector.service
    if [ $? -eq 0 ]; then
        break
    else
        echo "Starting retry again systemctl enable csiplugin-connector.service.retry count:$i"
        sleep 2
    fi
done

echo "Starting systemctl restart csiplugin-connector.service."
for((i=1;i<=10;i++));
do
    ${HOST_CMD} systemctl restart csiplugin-connector.service
    if [ $? -eq 0 ]; then
        break
    else
        echo "Starting retry again systemctl restart csiplugin-connector.service.retry count:$i"
        sleep 2
    fi
done

# start daemon
/bin/plugin.csi.alibabacloud.com $@

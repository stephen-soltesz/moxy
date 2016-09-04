#!/bin/bash

set -x
set -e

BUILD=${1:-Specify build directory}
BOOTSTRAP=${2:-Specify the initramfs base directory}
SSHKEY=${3:? Authorized keys file}

KERN=$( uname --kernel-release )

function unpack () {
  dir=$1
  url=$2
  tgz=$( basename $url )
  if ! test -d $dir ; then
    if ! test -f $tgz ; then
      wget $url
    fi
    tar -xvf $tgz
  fi
}

#debootstrap --arch amd64 xenial $BOOTSTRAP

cat <<EOF > $BOOTSTRAP/etc/resolv.conf
nameserver 8.8.8.8
EOF

cat <<EOF > $BOOTSTRAP/etc/fstab
# UNCONFIGURED FSTAB FOR BASE SYSTEM
proc           /proc        proc     nosuid,noexec,nodev 0     0
sysfs          /sys         sysfs    nosuid,noexec,nodev 0     0
EOF

cat <<\EOF > $BOOTSTRAP/etc/rc.local
#!/bin/sh

# Enable the mellanox something tools.
mst start

# Try to setup networking.
ping -c 1 www.google.com || (
    echo "KERNEL COMMAND LINE:"
    cat /proc/cmdline
    echo "^^^^^^^^^^^^^^^^^^^"
    IPCFG=$( cat /proc/cmdline | tr ' ' '\n' | grep ip= | sed -e 's/ip=//g' )
    if test -n "$IPCFG" ; then
        echo $IPCFG | tr ':' ' ' | ( read IP GW NM HN DEV _
            ifconfig $DEV $IP netmask $NM
            route add default gw $GW
            hostname $HN
        )
    else
        echo "Sorry -- no IP configuration. Trying to use default."
        ifconfig eth0 192.168.0.107 netmask 255.255.255.0
        route add default gw 192.168.0.1
    fi
)
EOF
chmod 755 $BOOTSTRAP/etc/rc.local


cat <<\EOF > $BOOTSTRAP/init
#!/bin/bash

/bin/mount -t proc proc /proc
/bin/mount -t sysfs sysfs /sys
/bin/mount -t devpts /dev/pts /dev/pts

/sbin/modprobe e1000
/sbin/modprobe mlx4_en

/etc/rc.local

/sbin/dropbear
mkdir -p /var/log
/usr/sbin/rsyslogd

echo "Dropping to a shell with job control. -- 3"
/usr/bin/setsid /bin/bash -c 'exec /bin/bash </dev/tty1 >/dev/tty1 2>&1'

echo "Sleeping for 6000"
sleep 6000

echo "Shell without job control."
exec /bin/bash
EOF
chmod 755 $BOOTSTRAP/init


if ! test -d $BOOTSTRAP/root/mft-4.4.0-44 ; then
    if ! test -f $BOOTSTRAP/usr/bin/flint ; then
        pushd $BUILD
            unpack mft-4.4.0-44 http://www.mellanox.com/downloads/MFT/mft-4.4.0-44.tgz
            cp -ar mft-4.4.0-44 $BOOTSTRAP/root
        popd
    fi
fi

# IN $BOOTSTRAP CHROOT
mount -t proc proc $BOOTSTRAP/proc
mount -t sysfs sysfs $BOOTSTRAP/sys
chroot $BOOTSTRAP /bin/bash <<EOF
    set -e
    set -x
    if ! test -f /usr/bin/flint ; then
        # Extra packages needed for correct operation.
        apt-get install -y usbutils pciutils perl-modules

        # For building dkms packages.
        apt-get install -y gcc make dkms linux-headers-generic linux-generic

        pushd /root/mft-4.4.0-44
            test -f /usr/bin/flint || ./install.sh
        popd

        # Unnecessary commands.
        apt-get autoremove -y linux-generic linux-headers-4.4.0-21 linux-headers-`uname -r`
        apt-get clean

        pushd /root
            rm -rf mft-4.4.0-44
        popd
        rm -rf /boot/*
	fi
EOF
# TODO: remove linux-firmware
# TODO: remove /var/cache/*
umount $BOOTSTRAP/proc
umount $BOOTSTRAP/sys


echo "Setting up directory hierarchy"
mkdir -p $BOOTSTRAP/etc/dropbear
cp $BUILD/dropbear/sbin/dropbear $BOOTSTRAP/sbin
cp $BUILD/dropbear/bin/scp $BOOTSTRAP/bin
cp $BUILD/keys/* $BOOTSTRAP/etc/dropbear

mkdir -p $BOOTSTRAP/root/.ssh
cp $SSHKEY $BOOTSTRAP/root/.ssh/authorized_keys
chown root:root $BOOTSTRAP/root/.ssh/authorized_keys
chmod 700 $BOOTSTRAP/root/

if ! test -f $BOOTSTRAP/usr/local/util/zbin ; then
  pushd $BUILD
    pushd ipxe/src
      # make bin/ipxe.iso EMBED=$BASEDIR/embed.ipxe,$BUILD/vmlinuz,$BUILD/initramfs  # DEBUG=basemem,hidemem,memmap,settings
      make util/zbin
      cp -ar util $BOOTSTRAP/usr/local/
      cp /vagrant/updaterom.sh $BOOTSTRAP/usr/local/util
      cp /vagrant/flashrom.sh $BOOTSTRAP/usr/local/util
    popd
  popd
fi

# bundle everything
# pushd /build/initramfs_base
pushd $BOOTSTRAP
  find . | cpio -H newc -o | gzip -c > $BUILD/initramfs-mlx
popd
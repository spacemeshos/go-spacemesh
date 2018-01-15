yum install -y git

curl https://dl.google.com/go/go1.9.2.linux-amd64.tar.gz | tar xvz
rm -rf /usr/local/go/
mv go /usr/local/

export GOPATH=/home/vagrant/spacemesh/
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin/

echo "export GOPATH=$GOPATH" >> /etc/profile
echo "export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin" >> /etc/profile

cd $GOPATH/src/github.com/spacemeshos/go-spacemesh

make devtools

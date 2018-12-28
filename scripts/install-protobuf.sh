#!/bin/bash -e
# Detect OS and architecture
if [[ $(uname -s) == "Linux" ]]; then
    os="linux";
elif [[ $(uname -s) == "Darwin" ]]; then # MacOS
    os="osx";
elif [[($OS == "Windows_NT" )]]; then # wind
	os="win32";
else
    exit 1;
fi


arch=$(uname -m)
if [[($OS == "Windows_NT" )]]; then
	protoc_url=https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-${os}.zip
else
	protoc_url=https://github.com/protocolbuffers/protobuf/releases/download/v3.6.1/protoc-3.6.1-${os}-${arch}.zip
fi
# Make sure you grab the latest version
echo "fetching ${protoc_url}"
curl -L -o "protoc.zip" ${protoc_url}

# Unzip
echo "extracting..."
unzip -u protoc.zip -d protoc3

# Move protoc to /usr/local/bin/
echo "moving bin/protoc to /usr/local/bin/protoc"
cp -rf protoc3/bin --target-directory=/usr/local/

# Move protoc3/include to /usr/local/include/
echo "syncing include to /usr/local/include"
cp -rfu  protoc3/include --target-directory=/usr/local/


# Cleanup

echo "cleaning up..."
rm protoc.zip
rm -rf protoc3
#ex
go get -u github.com/golang/protobuf/protoc-gen-go
rm -rf protobuf
git clone https://github.com/golang/protobuf
cd protobuf
#./regenerate.sh
make
make install
cd ..
rm -rf protobuf
echo "done"

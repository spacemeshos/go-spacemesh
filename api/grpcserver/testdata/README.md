# Test data

The files in this folder are only intended to be used for testing. They are not intended to be used in production.

## Certificates

The certificate files are used for testing TLS connections. They have been generated using the following commands:

```bash
# create certificate extensions to allow using them for localhost
cat > domains.ext <<EOF
[v3_req]
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF

# create CA private key and certificate
openssl req -x509 -newkey rsa:4096 -days 365 -nodes -keyout ca.key -out ca.crt -subj "/C=EN/ST=Spacemesh/L=Tel Aviv/O=Spacemesh/CN=spacemesh.io/emailAddress=info@spacemesh.io"

# create server private key and CSR
openssl req -newkey rsa:4096 -nodes -keyout server.key -out server-req.pem \
    -subj "/C=EN/ST=Spacemesh/L=Tel Aviv/O=Server/CN=server.spacemesh.io/emailAddress=info@spacemesh.io"

# use CA private key to sign ser CRS and get back the signed certificate
openssl x509 -req -in server-req.pem -days 60 -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -extfile domains.ext -extensions v3_req
rm server-req.pem

# create client private key and CSR
openssl req -newkey rsa:4096 -nodes -keyout client.key -out client-req.pem \
    -subj "/C=EN/ST=Spacemesh/L=Tel Aviv/O=Client/CN=client.spacemesh.io/emailAddress=info@spacemesh.io" \

# use CA private key to sign client CSR and get back the signed certificate
openssl x509 -req -in client-req.pem -days 60 -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -extfile domains.ext -extensions v3_req
rm client-req.pem
```

package wsock

import (
	"crypto/tls"
	"net"
)

func dialTLS(d *net.Dialer, host, serverName string) (net.Conn, error) {
	return tls.DialWithDialer(d, "tcp", host, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
}

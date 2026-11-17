package rtc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// LoadOrCreateCert: dùng certFile/keyFile nếu có; không thì tạo chứng chỉ tự ký (10 năm) cho
// localhost + tên máy + mọi IP của máy, lưu vào dir để lần sau dùng lại (trình duyệt chỉ
// phải "chấp nhận rủi ro" một lần).
func LoadOrCreateCert(dir, certFile, keyFile string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		return tls.LoadX509KeyPair(certFile, keyFile)
	}
	certFile, keyFile = filepath.Join(dir, "tls-cert.pem"), filepath.Join(dir, "tls-key.pem")
	if c, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil && hasAllIPs(leaf) {
			return c, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	host, _ := os.Hostname()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "Go Chat (LAN)", Organization: []string{"Go Chat"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", host},
		IPAddresses:  localIPs(),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err == nil {
		_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
		_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func localIPs() []net.IP {
	ips := []net.IP{net.IPv4(127, 0, 0, 1)}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			ips = append(ips, ipn.IP.To4())
		}
	}
	return ips
}

// hasAllIPs: chứng chỉ cũ còn đủ IP hiện tại của máy (đổi mạng thì tạo lại).
func hasAllIPs(c *x509.Certificate) bool {
	for _, ip := range localIPs() {
		found := false
		for _, have := range c.IPAddresses {
			if have.Equal(ip) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return time.Now().Before(c.NotAfter)
}

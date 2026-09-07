package provision

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func keyFixture() string {
	var out bytes.Buffer
	for _, field := range [][]byte{[]byte("ssh-ed25519"), bytes.Repeat([]byte{0x42}, 32)} {
		_ = binary.Write(&out, binary.BigEndian, uint32(len(field)))
		out.Write(field)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(out.Bytes())
}
func configFixture() (Config, domain.CreationSpec) {
	spec := domain.CreationSpec{UUID: "b2976600-52bf-4b8e-988e-51ed53008629", NICs: []domain.CreationNIC{{ID: "internet", MAC: "02:00:00:00:00:01", Link: "up"}, {ID: "lab", MAC: "fe:00:00:00:00:02", Link: "up"}}}
	config := Config{SourceDiskID: "boot", Profile: Profile, CloudInitCompatible: true, SourceSHA256: strings.Repeat("a", 64), SourceReference: "https://example.invalid/declared-image", Hostname: "cloud.example", UserName: "operator", AuthorizedKeys: []string{keyFixture()}, IPv6: "disabled", MediaID: "cloud-init", Interfaces: []Interface{{NICID: "internet", Role: "normal", Mode: "dhcp", DefaultRoute: true, RouteMetric: 100, DNS: []string{}}, {NICID: "lab", Role: "protected", Mode: "static", Address: "10.23.4.10/24", RouteMetric: 200, DNS: []string{}}}}
	return config, spec
}
func TestNoCloudIdentityAndRoutesAreExplicit(t *testing.T) {
	c, s := configFixture()
	files, err := Render(c, s)
	if err != nil {
		t.Fatal(err)
	}
	var user, meta, network map[string]any
	if err = json.Unmarshal(bytes.TrimPrefix(files["user-data"], []byte("#cloud-config\n")), &user); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(files["meta-data"], &meta); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(files["network-config"], &network); err != nil {
		t.Fatal(err)
	}
	if meta["instance-id"] != "virmill-"+s.UUID || meta["local-hostname"] != c.Hostname || user["ssh_pwauth"] != false || user["disable_root"] != true || user["ssh_deletekeys"] != true {
		t.Fatal("guest clone identity or auth defaults differ", user, meta)
	}
	if user["resize_rootfs"] != false || user["growpart"].(map[string]any)["mode"] != "off" || user["package_update"] != false || user["package_upgrade"] != false || user["package_reboot_if_required"] != false {
		t.Fatal("implicit disk growth, package upgrade or reboot", user)
	}
	users := user["users"].([]any)
	account := users[0].(map[string]any)
	if account["lock_passwd"] != true || account["sudo"] != nil || account["passwd"] != nil {
		t.Fatal("implicit password/root privilege", account)
	}
	ethernet := network["ethernets"].(map[string]any)
	internet := ethernet["internet"].(map[string]any)
	lab := ethernet["lab"].(map[string]any)
	if internet["match"].(map[string]any)["macaddress"] != s.NICs[0].MAC || lab["match"].(map[string]any)["macaddress"] != s.NICs[1].MAC || internet["dhcp4-overrides"].(map[string]any)["use-routes"] != true || lab["routes"] != nil || lab["nameservers"] != nil {
		t.Fatal("MAC mapping or default route intent lost", network)
	}
	for _, value := range ethernet {
		nic := value.(map[string]any)
		if nic["dhcp6"] != false || nic["accept-ra"] != false || len(nic["link-local"].([]any)) != 0 {
			t.Fatal("implicit IPv6 addressing", nic)
		}
	}
	s.UUID = "b2976600-52bf-4b8e-988e-51ed53008630"
	s.NICs[0].MAC = "06:00:00:00:00:03"
	other, err := Render(c, s)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(files["meta-data"], other["meta-data"]) || bytes.Equal(files["network-config"], other["network-config"]) {
		t.Fatal("seed reused old clone identity")
	}
}
func TestProtectedDHCPDoesNotSupplyRoutesOrDNS(t *testing.T) {
	c, s := configFixture()
	c.Interfaces[1].Mode = "dhcp"
	c.Interfaces[1].Address = ""
	files, err := Render(c, s)
	if err != nil {
		t.Fatal(err)
	}
	var network map[string]any
	if err = json.Unmarshal(files["network-config"], &network); err != nil {
		t.Fatal(err)
	}
	overrides := network["ethernets"].(map[string]any)["lab"].(map[string]any)["dhcp4-overrides"].(map[string]any)
	for _, key := range []string{"use-routes", "use-dns", "use-domains", "use-hostname"} {
		if overrides[key] != false {
			t.Fatal("protected DHCP can replace normal guest policy", overrides)
		}
	}
}
func TestNoCloudRefusesAmbiguousUnsafeOrUnsupportedIntent(t *testing.T) {
	for _, change := range []func(*Config, *domain.CreationSpec){func(c *Config, s *domain.CreationSpec) { c.CloudInitCompatible = false }, func(c *Config, s *domain.CreationSpec) { c.Profile = "unknown" }, func(c *Config, s *domain.CreationSpec) { c.IPv6 = "automatic" }, func(c *Config, s *domain.CreationSpec) {
		c.SourceReference = "https://name:secret@example.invalid/image"
	}, func(c *Config, s *domain.CreationSpec) {
		c.SourceReference = "https://example.invalid/image?token=secret"
	}, func(c *Config, s *domain.CreationSpec) { c.UserName = "root" }, func(c *Config, s *domain.CreationSpec) { c.Hostname = "a\nwrite_files: injected" }, func(c *Config, s *domain.CreationSpec) { c.AuthorizedKeys = []string{"-----BEGIN PRIVATE KEY-----"} }, func(c *Config, s *domain.CreationSpec) { c.Interfaces = c.Interfaces[:1] }, func(c *Config, s *domain.CreationSpec) { c.Interfaces[1].NICID = "internet" }, func(c *Config, s *domain.CreationSpec) { c.Interfaces[1].DefaultRoute = true }, func(c *Config, s *domain.CreationSpec) { c.Interfaces[1].Gateway = "10.23.4.1" }, func(c *Config, s *domain.CreationSpec) { c.Interfaces[1].Address = "10.23.4.255/24" }, func(c *Config, s *domain.CreationSpec) { c.Interfaces[1].DNS = []string{"10.23.4.1"} }, func(c *Config, s *domain.CreationSpec) { s.NICs[0].Link = "down" }, func(c *Config, s *domain.CreationSpec) { s.NICs[0].MAC = "ff:00:00:00:00:01" }, func(c *Config, s *domain.CreationSpec) { s.NICs[1].MAC = s.NICs[0].MAC }} {
		c, s := configFixture()
		change(&c, &s)
		if err := Validate(c, s); err == nil {
			t.Fatal("unsafe configuration accepted", c, s)
		}
	}
}
func TestPublicKeyWireValidationRejectsPrivateOptionsAndMalformedKeys(t *testing.T) {
	key := keyFixture()
	if got, err := PublicKey(key); err != nil || got != key {
		t.Fatal(got, err)
	}
	for _, bad := range []string{key + " comment", "command=\"id\" " + key, strings.Replace(key, "ssh-ed25519", "ssh-rsa", 1), key[:len(key)-3], key + "\n", "-----BEGIN OPENSSH PRIVATE KEY-----"} {
		if _, err := PublicKey(bad); err == nil {
			t.Fatal("invalid key accepted", bad)
		}
	}
}

func publicKeyFields(kind string, fields ...[]byte) string {
	var out bytes.Buffer
	for _, field := range append([][]byte{[]byte(kind)}, fields...) {
		_ = binary.Write(&out, binary.BigEndian, uint32(len(field)))
		out.Write(field)
	}
	return kind + " " + base64.StdEncoding.EncodeToString(out.Bytes())
}
func positiveInteger(b []byte) []byte {
	if b[0]&0x80 != 0 {
		return append([]byte{0}, b...)
	}
	return b
}
func TestRSAAndECDSAPublicKeysRequireValidWireStructure(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	exponent := positiveInteger(big.NewInt(int64(key.E)).Bytes())
	modulus := positiveInteger(key.N.Bytes())
	if _, err = PublicKey(publicKeyFields("ssh-rsa", exponent, modulus)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{publicKeyFields("ssh-rsa", []byte{2}, modulus), publicKeyFields("ssh-rsa", []byte{0x80}, modulus), publicKeyFields("ssh-rsa", exponent, []byte{1}), publicKeyFields("ssh-rsa", exponent, modulus, []byte("trailing"))} {
		if _, err = PublicKey(bad); err == nil {
			t.Fatal("malformed RSA wire key accepted")
		}
	}
	for _, test := range []struct {
		name  string
		curve elliptic.Curve
	}{{"nistp256", elliptic.P256()}, {"nistp384", elliptic.P384()}, {"nistp521", elliptic.P521()}} {
		key, err := ecdsa.GenerateKey(test.curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		point := elliptic.Marshal(test.curve, key.X, key.Y)
		if _, err = PublicKey(publicKeyFields("ecdsa-sha2-"+test.name, []byte(test.name), point)); err != nil {
			t.Fatal(err)
		}
		if _, err = PublicKey(publicKeyFields("ecdsa-sha2-"+test.name, []byte("wrong"), point)); err == nil {
			t.Fatal("curve mismatch accepted")
		}
		if _, err = PublicKey(publicKeyFields("ecdsa-sha2-"+test.name, []byte(test.name), []byte{4, 0})); err == nil {
			t.Fatal("invalid curve point accepted")
		}
	}
}
func FuzzPublicKey(f *testing.F) {
	f.Add(keyFixture())
	f.Add("-----BEGIN OPENSSH PRIVATE KEY-----")
	f.Add("ssh-rsa AAAAB3NzaC1yc2E=")
	f.Fuzz(func(t *testing.T, s string) { _, _ = PublicKey(s) })
}

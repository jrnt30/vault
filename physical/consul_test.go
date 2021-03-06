package physical

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/vault/helper/strutil"
	"github.com/ory-am/dockertest"
)

type consulConf map[string]string

var (
	addrCount     int = 0
	testImagePull sync.Once
)

func testHostIP() string {
	a := addrCount
	addrCount++
	return fmt.Sprintf("127.0.0.%d", a)
}

func testConsulBackend(t *testing.T) *ConsulBackend {
	return testConsulBackendConfig(t, &consulConf{})
}

func testConsulBackendConfig(t *testing.T, conf *consulConf) *ConsulBackend {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	be, err := newConsulBackend(*conf, logger)
	if err != nil {
		t.Fatalf("Expected Consul to initialize: %v", err)
	}

	c, ok := be.(*ConsulBackend)
	if !ok {
		t.Fatalf("Expected ConsulBackend")
	}

	return c
}

func testConsul_testConsulBackend(t *testing.T) {
	c := testConsulBackend(t)
	if c == nil {
		t.Fatalf("bad")
	}
}

func testActiveFunc(activePct float64) activeFunction {
	return func() bool {
		var active bool
		standbyProb := rand.Float64()
		if standbyProb > activePct {
			active = true
		}
		return active
	}
}

func testSealedFunc(sealedPct float64) sealedFunction {
	return func() bool {
		var sealed bool
		unsealedProb := rand.Float64()
		if unsealedProb > sealedPct {
			sealed = true
		}
		return sealed
	}
}

func TestConsul_ServiceTags(t *testing.T) {
	consulConfig := map[string]string{
		"path":                 "seaTech/",
		"service":              "astronomy",
		"service_tags":         "deadbeef, cafeefac, deadc0de, feedface",
		"redirect_addr":        "http://127.0.0.2:8200",
		"check_timeout":        "6s",
		"address":              "127.0.0.2",
		"scheme":               "https",
		"token":                "deadbeef-cafeefac-deadc0de-feedface",
		"max_parallel":         "4",
		"disable_registration": "false",
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	be, err := newConsulBackend(consulConfig, logger)
	if err != nil {
		t.Fatal(err)
	}

	c, ok := be.(*ConsulBackend)
	if !ok {
		t.Fatalf("failed to create physical Consul backend")
	}

	expected := []string{"deadbeef", "cafeefac", "deadc0de", "feedface"}
	actual := c.fetchServiceTags(false)
	if !strutil.EquivalentSlices(actual, append(expected, "standby")) {
		t.Fatalf("bad: expected:%s actual:%s", append(expected, "standby"), actual)
	}

	actual = c.fetchServiceTags(true)
	if !strutil.EquivalentSlices(actual, append(expected, "active")) {
		t.Fatalf("bad: expected:%s actual:%s", append(expected, "active"), actual)
	}
}

func TestConsul_newConsulBackend(t *testing.T) {
	tests := []struct {
		name         string
		consulConfig map[string]string
		fail         bool
		redirectAddr string
		checkTimeout time.Duration
		path         string
		service      string
		address      string
		scheme       string
		token        string
		max_parallel int
		disableReg   bool
	}{
		{
			name:         "Valid default config",
			consulConfig: map[string]string{},
			checkTimeout: 5 * time.Second,
			redirectAddr: "http://127.0.0.1:8200",
			path:         "vault/",
			service:      "vault",
			address:      "127.0.0.1:8500",
			scheme:       "http",
			token:        "",
			max_parallel: 4,
			disableReg:   false,
		},
		{
			name: "Valid modified config",
			consulConfig: map[string]string{
				"path":                 "seaTech/",
				"service":              "astronomy",
				"redirect_addr":        "http://127.0.0.2:8200",
				"check_timeout":        "6s",
				"address":              "127.0.0.2",
				"scheme":               "https",
				"token":                "deadbeef-cafeefac-deadc0de-feedface",
				"max_parallel":         "4",
				"disable_registration": "false",
			},
			checkTimeout: 6 * time.Second,
			path:         "seaTech/",
			service:      "astronomy",
			redirectAddr: "http://127.0.0.2:8200",
			address:      "127.0.0.2",
			scheme:       "https",
			token:        "deadbeef-cafeefac-deadc0de-feedface",
			max_parallel: 4,
		},
		{
			name: "check timeout too short",
			fail: true,
			consulConfig: map[string]string{
				"check_timeout": "99ms",
			},
		},
	}

	for _, test := range tests {
		logger := log.New(os.Stderr, "", log.LstdFlags)
		be, err := newConsulBackend(test.consulConfig, logger)
		if test.fail {
			if err == nil {
				t.Fatalf(`Expected config "%s" to fail`, test.name)
			} else {
				continue
			}
		} else if !test.fail && err != nil {
			t.Fatalf("Expected config %s to not fail: %v", test.name, err)
		}

		c, ok := be.(*ConsulBackend)
		if !ok {
			t.Fatalf("Expected ConsulBackend: %s", test.name)
		}
		c.disableRegistration = true

		if c.disableRegistration == false {
			addr := os.Getenv("CONSUL_HTTP_ADDR")
			if addr == "" {
				continue
			}
		}

		var shutdownCh ShutdownChannel
		waitGroup := &sync.WaitGroup{}
		if err := c.RunServiceDiscovery(waitGroup, shutdownCh, test.redirectAddr, testActiveFunc(0.5), testSealedFunc(0.5)); err != nil {
			t.Fatalf("bad: %v", err)
		}

		if test.checkTimeout != c.checkTimeout {
			t.Errorf("bad: %v != %v", test.checkTimeout, c.checkTimeout)
		}

		if test.path != c.path {
			t.Errorf("bad: %s %v != %v", test.name, test.path, c.path)
		}

		if test.service != c.serviceName {
			t.Errorf("bad: %v != %v", test.service, c.serviceName)
		}

		// FIXME(sean@): Unable to test max_parallel
		// if test.max_parallel != cap(c.permitPool) {
		// 	t.Errorf("bad: %v != %v", test.max_parallel, cap(c.permitPool))
		// }
	}
}

func TestConsul_serviceTags(t *testing.T) {
	tests := []struct {
		active bool
		tags   []string
	}{
		{
			active: true,
			tags:   []string{"active"},
		},
		{
			active: false,
			tags:   []string{"standby"},
		},
	}

	c := testConsulBackend(t)

	for _, test := range tests {
		tags := c.fetchServiceTags(test.active)
		if !reflect.DeepEqual(tags[:], test.tags[:]) {
			t.Errorf("Bad %v: %v %v", test.active, tags, test.tags)
		}
	}
}

func TestConsul_setRedirectAddr(t *testing.T) {
	tests := []struct {
		addr string
		host string
		port int64
		pass bool
	}{
		{
			addr: "http://127.0.0.1:8200/",
			host: "127.0.0.1",
			port: 8200,
			pass: true,
		},
		{
			addr: "http://127.0.0.1:8200",
			host: "127.0.0.1",
			port: 8200,
			pass: true,
		},
		{
			addr: "https://127.0.0.1:8200",
			host: "127.0.0.1",
			port: 8200,
			pass: true,
		},
		{
			addr: "unix:///tmp/.vault.addr.sock",
			host: "/tmp/.vault.addr.sock",
			port: -1,
			pass: true,
		},
		{
			addr: "127.0.0.1:8200",
			pass: false,
		},
		{
			addr: "127.0.0.1",
			pass: false,
		},
	}
	for _, test := range tests {
		c := testConsulBackend(t)
		err := c.setRedirectAddr(test.addr)
		if test.pass {
			if err != nil {
				t.Fatalf("bad: %v", err)
			}
		} else {
			if err == nil {
				t.Fatalf("bad, expected fail")
			} else {
				continue
			}
		}

		if c.redirectHost != test.host {
			t.Fatalf("bad: %v != %v", c.redirectHost, test.host)
		}

		if c.redirectPort != test.port {
			t.Fatalf("bad: %v != %v", c.redirectPort, test.port)
		}
	}
}

func TestConsul_NotifyActiveStateChange(t *testing.T) {
	c := testConsulBackend(t)

	if err := c.NotifyActiveStateChange(); err != nil {
		t.Fatalf("bad: %v", err)
	}
}

func TestConsul_NotifySealedStateChange(t *testing.T) {
	c := testConsulBackend(t)

	if err := c.NotifySealedStateChange(); err != nil {
		t.Fatalf("bad: %v", err)
	}
}

func TestConsul_serviceID(t *testing.T) {
	passingTests := []struct {
		name         string
		redirectAddr string
		serviceName  string
		expected     string
	}{
		{
			name:         "valid host w/o slash",
			redirectAddr: "http://127.0.0.1:8200",
			serviceName:  "sea-tech-astronomy",
			expected:     "sea-tech-astronomy:127.0.0.1:8200",
		},
		{
			name:         "valid host w/ slash",
			redirectAddr: "http://127.0.0.1:8200/",
			serviceName:  "sea-tech-astronomy",
			expected:     "sea-tech-astronomy:127.0.0.1:8200",
		},
		{
			name:         "valid https host w/ slash",
			redirectAddr: "https://127.0.0.1:8200/",
			serviceName:  "sea-tech-astronomy",
			expected:     "sea-tech-astronomy:127.0.0.1:8200",
		},
	}

	for _, test := range passingTests {
		c := testConsulBackendConfig(t, &consulConf{
			"service": test.serviceName,
		})

		if err := c.setRedirectAddr(test.redirectAddr); err != nil {
			t.Fatalf("bad: %s %v", test.name, err)
		}

		serviceID := c.serviceID()
		if serviceID != test.expected {
			t.Fatalf("bad: %v != %v", serviceID, test.expected)
		}
	}
}

func TestConsulBackend(t *testing.T) {
	var token string
	addr := os.Getenv("CONSUL_HTTP_ADDR")
	if addr == "" {
		cid, connURL := prepareTestContainer(t)
		if cid != "" {
			defer cleanupTestContainer(t, cid)
		}
		addr = connURL
		token = dockertest.ConsulACLMasterToken
	}

	conf := api.DefaultConfig()
	conf.Address = addr
	conf.Token = token
	client, err := api.NewClient(conf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	randPath := fmt.Sprintf("vault-%d/", time.Now().Unix())
	defer func() {
		client.KV().DeleteTree(randPath, nil)
	}()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	b, err := NewBackend("consul", logger, map[string]string{
		"address":      conf.Address,
		"path":         randPath,
		"max_parallel": "256",
		"token":        conf.Token,
	})
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	testBackend(t, b)
	testBackend_ListPrefix(t, b)
}

func TestConsulHABackend(t *testing.T) {
	var token string
	addr := os.Getenv("CONSUL_HTTP_ADDR")
	if addr == "" {
		cid, connURL := prepareTestContainer(t)
		if cid != "" {
			defer cleanupTestContainer(t, cid)
		}
		addr = connURL
		token = dockertest.ConsulACLMasterToken
	}

	conf := api.DefaultConfig()
	conf.Address = addr
	conf.Token = token
	client, err := api.NewClient(conf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	randPath := fmt.Sprintf("vault-%d/", time.Now().Unix())
	defer func() {
		client.KV().DeleteTree(randPath, nil)
	}()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	b, err := NewBackend("consul", logger, map[string]string{
		"address":      conf.Address,
		"path":         randPath,
		"max_parallel": "-1",
		"token":        conf.Token,
	})
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	ha, ok := b.(HABackend)
	if !ok {
		t.Fatalf("consul does not implement HABackend")
	}
	testHABackend(t, ha, ha)

	detect, ok := b.(RedirectDetect)
	if !ok {
		t.Fatalf("consul does not implement RedirectDetect")
	}
	host, err := detect.DetectHostAddr()
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	if host == "" {
		t.Fatalf("bad addr: %v", host)
	}
}

func prepareTestContainer(t *testing.T) (cid dockertest.ContainerID, retAddress string) {
	if os.Getenv("CONSUL_HTTP_ADDR") != "" {
		return "", os.Getenv("CONSUL_HTTP_ADDR")
	}

	// Without this the checks for whether the container has started seem to
	// never actually pass. There's really no reason to expose the test
	// containers, so don't.
	dockertest.BindDockerToLocalhost = "yep"

	testImagePull.Do(func() {
		dockertest.Pull(dockertest.ConsulImageName)
	})

	try := 0
	cid, connErr := dockertest.ConnectToConsul(60, 500*time.Millisecond, func(connAddress string) bool {
		try += 1
		// Build a client and verify that the credentials work
		config := api.DefaultConfig()
		config.Address = connAddress
		config.Token = dockertest.ConsulACLMasterToken
		client, err := api.NewClient(config)
		if err != nil {
			if try > 50 {
				panic(err)
			}
			return false
		}

		_, err = client.KV().Put(&api.KVPair{
			Key:   "setuptest",
			Value: []byte("setuptest"),
		}, nil)
		if err != nil {
			if try > 50 {
				panic(err)
			}
			return false
		}

		retAddress = connAddress
		return true
	})

	if connErr != nil {
		t.Fatalf("could not connect to consul: %v", connErr)
	}

	return
}

func cleanupTestContainer(t *testing.T, cid dockertest.ContainerID) {
	err := cid.KillRemove()
	if err != nil {
		t.Fatal(err)
	}
}

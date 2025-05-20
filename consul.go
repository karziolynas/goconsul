package goconsul

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	ttl     = time.Second * 8
	checkID = "check"
)

type Service struct {
	consulClient *api.Client
	config       *api.Config
	id           string
	name         string
	address      string
	port         int
	tags         []string
}

// Registers a new consul client based on provided arguments.
// Returns a Service struct with a pointer to the consul client.
func NewService(consulAddress string, serviceID string, serviceName string, address string, port int, tags []string) *Service {
	caCert, err := os.ReadFile("/go/src/weather_service/certs/consul-agent-ca.pem")
	if err != nil {
		log.Fatalf("Failed to read CA file: %v", err)
	}
	caPool := x509.NewCertPool()
	okBool := caPool.AppendCertsFromPEM(caCert)
	if !okBool {
		log.Println("Error appending certs")
	}

	//Loads CLIENT cert and key
	cert, err := tls.LoadX509KeyPair(
		"/go/src/weather_service/certs/dc1-client-consul-0.pem",
		"/go/src/weather_service/certs/dc1-client-consul-0-key.pem",
	)
	if err != nil {
		log.Fatalf("Failed to load client cert/key: %v", err)
	}

	tlsConfig := &tls.Config{
		RootCAs:      caPool,
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	config := api.DefaultConfig()
	config.Address = consulAddress
	config.Scheme = "https"
	config.HttpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	consulClient, err := api.NewClient(config)
	if err != nil {
		log.Println("Failed creating client")
		return nil
	}

	return &Service{
		consulClient: consulClient,
		config:       config,
		id:           serviceID,
		name:         serviceName,
		address:      address,
		port:         port,
		tags:         tags,
	}
}

// Registers the service to consul and starts the basic TTL health check.
func (s *Service) Start(consulAddress, serviceAddr /*, containerName*/ string) {
	var wg sync.WaitGroup
	wg.Add(1)
	s.ServiceIDCheck(&s.id, s.name)
	s.registerServiceConsul(serviceAddr)

	go s.updateHealthCheck()

	go s.startPerformanceChecks()
	wg.Wait()
}

// Registers the service to consul.
func (s *Service) registerServiceConsul(serviceAddr string) {
	var httpEndpoint, tcpAddress string

	if strings.HasPrefix(serviceAddr, "http") {
		httpEndpoint = serviceAddr
		parsedURL, err := url.Parse(serviceAddr)
		if err != nil {
			log.Printf("Warning: Could not parse URL %s: %v", serviceAddr, err)
			tcpAddress = s.address + ":" + strconv.Itoa(s.port)
		} else {
			tcpAddress = parsedURL.Host
		}
	} else {
		httpEndpoint = fmt.Sprintf("http://%s:%d%s", s.address, s.port, serviceAddr)
		tcpAddress = fmt.Sprintf("%s:%d", s.address, s.port)
	}

	CheckTTL := &api.AgentCheckRegistration{
		ID:        checkID + "_TTL_" + s.id,
		Name:      "TTL Health Check",
		ServiceID: s.id,
		AgentServiceCheck: api.AgentServiceCheck{
			//CheckID:                checkID + "_TTL_" + s.id,
			TLSSkipVerify:          true,
			TTL:                    ttl.String(),
			FailuresBeforeWarning:  1,
			FailuresBeforeCritical: 3,
		},
	}
	CheckHTTP := &api.AgentCheckRegistration{
		ID:        checkID + "_HTTP_" + s.id,
		Name:      "HTTP Health Check",
		ServiceID: s.id,
		AgentServiceCheck: api.AgentServiceCheck{
			//CheckID:                checkID + "_HTTP_" + s.id,
			HTTP:                   httpEndpoint,
			TLSSkipVerify:          true,
			Method:                 "GET",
			Interval:               "30s",
			Status:                 "passing",
			FailuresBeforeWarning:  1,
			FailuresBeforeCritical: 3,
		},
	}
	CheckTCP := &api.AgentCheckRegistration{
		ID:        checkID + "_TCP_" + s.id,
		Name:      "TCP Health Check",
		ServiceID: s.id,
		AgentServiceCheck: api.AgentServiceCheck{
			//CheckID:                checkID + "_TCP_" + s.id,
			TCP:                    tcpAddress,
			TLSSkipVerify:          true,
			Interval:               "30s",
			Status:                 "passing",
			Timeout:                "5s",
			FailuresBeforeWarning:  1,
			FailuresBeforeCritical: 3,
		},
	}

	register := &api.AgentServiceRegistration{
		ID:      s.id,
		Name:    s.name,
		Tags:    s.tags,
		Address: s.address,
		Port:    s.port,
	}

	if err := s.consulClient.Agent().ServiceRegister(register); err != nil {
		log.Fatal(err)
	}

	s.consulClient.Agent().CheckRegister(CheckTTL)
	s.consulClient.Agent().CheckRegister(CheckHTTP)
	s.consulClient.Agent().CheckRegister(CheckTCP)

	fmt.Printf("Service - '%v' registered! \n", s.id)
}

// Updates the service's health/TTL.
func (s *Service) updateHealthCheck() {
	checkId := checkID + "_TTL_" + s.id
	ticker := time.NewTicker(time.Second * 5)
	for {
		err := s.consulClient.Agent().UpdateTTL(checkId, "Service TTL updated", api.HealthPassing)
		if err != nil {
			log.Println(err)
		}
		<-ticker.C
		fmt.Printf("Service TTL updated! \n")
	}

}

func (s *Service) startPerformanceChecks() {
	kv := s.consulClient.KV()
	time.Sleep(10 * time.Second)
	ticker := time.NewTicker(time.Minute * 3)
	for {
		// usage, _ := os.ReadFile("/sys/fs/cgroup/memory/memory.usage_in_bytes")
		// memBytes, _ := strconv.ParseInt(strings.TrimSpace(string(usage)), 10, 64)
		// memMB := float64(memBytes) / (1024 * 1024) //converting to MB
		// log.Println("Memory usage (MB): ", memMB)

		// t1 := time.Now()
		// cpu1, errCpu := readCpu()
		// if errCpu != nil {
		// 	log.Println("err cpu", errCpu)
		// }
		// log.Println("Cpu1: ", cpu1)

		// time.Sleep(5 * time.Second)

		// t2 := time.Now()
		// cpu2, _ := readCpu()
		// log.Println("Cpu2: ", cpu2)

		// delta := float64(cpu2-cpu1) / 1_000_000.0
		// log.Println("delta cpu: ", delta)
		// deltaTime := t2.Sub(t1).Seconds()
		// log.Println("delta time cpu: ", deltaTime)
		// cpuNumber := float64(runtime.NumCPU())
		// log.Println("corecount: ", cpuNumber)

		// cpuPercent := (delta / (deltaTime * cpuNumber)) * 100.0
		// log.Println("CPU usage (percentage) : ", cpuPercent)

		v, _ := mem.VirtualMemory()
		memMB := float64(v.Used) / (1024 * 1024)

		cpuPercent, _ := cpu.Percent(time.Second*10, false)

		data := map[string]float64{
			"cpu": cpuPercent[0],
			"mem": memMB,
		}
		//turns data into json
		jsonData, err := json.Marshal(data)
		if err != nil {
			log.Fatal(err)
		}

		p := &api.KVPair{
			Key:   s.id,
			Value: jsonData,
		}
		//saves it as a key-value pair
		_, err = kv.Put(p, nil)
		if err != nil {
			log.Println(err)
		}

		<-ticker.C
	}

}

func (s *Service) ServiceAddressCheck(port *int) {
	services, err := s.consulClient.Agent().Services()
	if err != nil {
		log.Fatal(err)
	}
	for _, entry := range services {
		if *port == entry.Port {
			*port++
			fmt.Printf("Port - %v already in use, changed to - %v \n", entry.Port, *port)
		}
	}

}

// testing if automating a pick for service ID is worth it
// method would check if service id exists and change it to something else
func (s *Service) ServiceIDCheck(id *string, name string) {
	services, _, err := s.consulClient.Health().Service(name, "", true, nil)
	if err != nil {
		log.Fatal(err)
	}
	baseValue := 1
	for _, entry := range services {
		if *id == entry.Service.ID {
			*id = strings.Replace(*id, strconv.Itoa(baseValue), strconv.Itoa(baseValue+1), 1)
			fmt.Printf("ID - %s already in use, changed to - %s \n", entry.Service.ID, *id)
		}
		baseValue++
	}
}

func (s *Service) ServiceDiscovery() {
	services, err := s.consulClient.Agent().Services()
	if err != nil {
		log.Fatal(err)
	}

	for _, entry := range services {
		fmt.Printf("Service: %s | Address: %s | Port: %d \n",
			entry.ID, entry.Address, entry.Port)
	}
}

func (s *Service) ServiceCatalog() {
	services /*meta*/, _, err := s.consulClient.Catalog().Services(nil)
	if err != nil {
		log.Fatal(err)
	}

	// Print the retrieved services
	fmt.Println("Services registered in Consul:")
	for service, tags := range services {
		fmt.Printf("- %s (Tags: %v)\n", service, tags)
	}

	// Print metadata (useful for debugging)
	//fmt.Printf("Query Metadata: %+v\n", meta)

}

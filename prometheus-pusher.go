package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/ShowMax/go-fqdn"
	"github.com/achun/tom-toml"
	"github.com/mgutz/logxi/v1"
)

type pusherConfig struct {
	PushGatewayURL string
	PushInterval   time.Duration
	Metrics        []metricConfig
}

type metricConfig struct {
	Name string
	URL  string
}

var logger log.Logger

func main() {
	path := flag.String("config", "/etc/prometheus-pusher/conf.d", "Config file or directory. If directory is specified then all files in the directory will be loaded.")
	flag.Parse()

	logger = log.New("prometheus-pusher")

	pusher, err := parseConfig(*path)
	if err != nil {
		logger.Error("Error parsing configuration", err.Error())
	}

	hostname := fqdn.Get()
	logger.Info("Starting prometheus-pusher", "instance_name", hostname)

	for _ = range time.Tick(pusher.PushInterval) {
		for _, metric := range pusher.Metrics {
			go getAndPush(metric.Name, metric.URL, pusher.PushGatewayURL, hostname)
		}
	}
}

func getConfigFiles(path string) []string {
	var files []string

	pathCheck, err := os.Open(path)
	if err != nil {
		logger.Fatal("Unable to open configuration file(s)", "error", err.Error())
	}

	pathInfo, err := pathCheck.Stat()
	if err != nil {
		logger.Fatal("Unable to stat configuration file(s)", "error", err.Error())
	}

	if pathInfo.IsDir() {
		dir, _ := pathCheck.Readdir(-1)
		for _, file := range dir {
			if file.Mode().IsRegular() {
				files = append(files, path+"/"+file.Name())
			}
		}
	} else {
		files = []string{path}
	}
	return files
}

func parseConfig(path string) (pusherConfig, error) {
	conf := pusherConfig{
		PushGatewayURL: "http://localhost:9091",
		PushInterval:   time.Duration(60 * time.Second),
		Metrics:        []metricConfig{},
	}

	for _, file := range getConfigFiles(path) {
		tomlFile, err := toml.LoadFile(file)
		if err != nil {
			return conf, err
		}

		metrics, _ := tomlFile.TableNames()
		for _, metric := range metrics {

			if metric == "config" {

				switch {
				case tomlFile["config.pushgateway_url"].IsValue():
					conf.PushGatewayURL = tomlFile["config.pushgateway_url"].String()

				case tomlFile["config.push_interval"].IsValue():
					interval := tomlFile["config.push_interval"].Int()
					conf.PushInterval = time.Duration(interval) * time.Second

				default:
					logger.Warn("Unknown configuration field", "config_section", metric)
				}

			} else {

				var port int
				host := "localhost"
				path := "/metrics"
				scheme := "http"

				switch {
				case tomlFile[metric+".host"].IsValue():
					host = tomlFile[metric+".host"].String()

				case tomlFile[metric+".path"].IsValue():
					path = tomlFile[metric+".path"].String()

				case tomlFile[metric+".ssl"].IsValue():
					if tomlFile[metric+".ssl"].Boolean() {
						scheme = "https"
					}

				case tomlFile[metric+".port"].IsValue():
					port = tomlFile[metric+".port"].Integer()

				default:
					logger.Warn("Unknown configuration field", "config_section", metric)
				}

				if port == 0 {
					logger.Fatal("Port is not defined", "config_section", metric)
				}

				conf.Metrics = append(conf.Metrics, metricConfig{
					Name: metric,
					URL:  fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path),
				})
			}
		}
	}

	return conf, nil
}

func getMetrics(metricURL string) []byte {
	logger.Info("Getting Node Exporter metrics", "url", metricURL)

	resp, err := http.Get(metricURL)
	if err != nil {
		logger.Error(err.Error(), "error", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Error(err.Error(), "error", err)
		return nil
	}
	return body
}

func pushMetrics(metricName string, pushgatewayURL string, instance string, metrics []byte) {
	postURL := fmt.Sprintf("%s/metrics/job/%s/instance/%s", pushgatewayURL, metricName, instance)
	logger.Info("Pushing Node exporter metrics", "endpoint", postURL)

	data := bytes.NewReader(metrics)
	resp, err := http.Post(postURL, "text/plain", data)
	if err != nil {
		logger.Error(err.Error(), "error", err)
		return
	}
	defer resp.Body.Close()
}

func getAndPush(metricName string, metricURL string, pushgatewayURL string, instance string) {
	if metrics := getMetrics(metricURL); metrics != nil {
		pushMetrics(metricName, pushgatewayURL, instance, metrics)
	}
}

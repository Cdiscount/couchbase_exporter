// Copyright 2018 Adel Abdelhak.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE.txt file.

package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/blakelead/couchbase_exporter/collector"
	yaml "gopkg.in/yaml.v2"

	d "github.com/coreos/go-systemd/daemon"
	log "github.com/sirupsen/logrus"
)

var (
	version = "0.5.1-5-g9bf7fc4"

	dbUser        = ""
	dbPwd         = ""
	listenAddr    = flag.String("web.listen-address", ":9191", "The address to listen on for HTTP requests.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	dbURI         = flag.String("db.uri", "http://localhost:8091", "The address of Couchbase cluster.")
	logLevel      = flag.String("log.level", "info", "Log level: info, debug, warn, error, fatal.")
	logFormat     = flag.String("log.format", "text", "Log format: text or json.")
	scrapeCluster = flag.Bool("scrape.cluster", true, "If false, cluster metrics wont be scraped.")
	scrapeNode    = flag.Bool("scrape.node", true, "If false, node metrics wont be scraped.")
	scrapeBucket  = flag.Bool("scrape.bucket", true, "If false, bucket metrics wont be scraped.")
	scrapeXDCR    = flag.Bool("scrape.xdcr", true, "If false, XDCR metrics wont be scraped.")

	validVersions = []string{"4.5.1", "5.1.1"}
)

func main() {

	log.SetFormatter(setLogFormat())
	log.SetOutput(os.Stdout)
	log.SetLevel(setLogLevel())

	log.Info("Couchbase Exporter Version ", version)

	loadConfFile()
	lookupEnv()
	flag.Parse()
	checkCredentials()

	context := collector.Context{URI: *dbURI, Username: dbUser, Password: dbPwd}

	getCouchbaseVersion(&context)

	collector.InitExporters(context, *scrapeCluster, *scrapeNode, *scrapeBucket, *scrapeXDCR)

	http.Handle(*metricsPath, promhttp.Handler())
	if *metricsPath != "/" {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`
			<html>
			<head><title>Couchbase Exporter</title></head>
			<body>
			<h1>Couchbase Exporter</h1>
			<p><i>by blakelead</i></p><br>
			<p>See <a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
		})
	}

	systemdSettings()

	// custom server used to set timeouts
	httpSrv := &http.Server{
		Addr:         *listenAddr,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Info("Listening at ", *listenAddr)
	log.Fatal(httpSrv.ListenAndServe())
}

func lookupEnv() {
	if val, ok := os.LookupEnv("CB_EXPORTER_LISTEN_ADDR"); ok {
		*listenAddr = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_TELEMETRY_PATH"); ok {
		*metricsPath = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_DB_URI"); ok {
		*dbURI = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_DB_USER"); ok {
		dbUser = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_DB_PASSWORD"); ok {
		dbPwd = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_LOG_LEVEL"); ok {
		*logLevel = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_LOG_FORMAT"); ok {
		*logFormat = val
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_SCRAPE_CLUSTER"); ok {
		*scrapeCluster, _ = strconv.ParseBool(val)
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_SCRAPE_NODE"); ok {
		*scrapeNode, _ = strconv.ParseBool(val)
	}
	if val, ok := os.LookupEnv("CB_EXPORTER_SCRAPE_BUCKET"); ok {
		*scrapeBucket, _ = strconv.ParseBool(val)
	}
}

func loadConfFile() {
	type confStruct struct {
		Web struct {
			ListenAddress string `json:"listenAddress" yaml:"listenAddress"`
			TelemetryPath string `json:"telemetryPath" yaml:"telemetryPath"`
		} `json:"web" yaml:"web"`
		DB struct {
			User     string `json:"user" yaml:"user"`
			Password string `json:"password" yaml:"password"`
			URI      string `json:"uri" yaml:"uri"`
		} `json:"db" yaml:"db"`
		Log struct {
			Level  string `json:"level" yaml:"level"`
			Format string `json:"format" yaml:"format"`
		} `json:"log" yaml:"log"`
		Scrape struct {
			Cluster bool `json:"cluster" yaml:"cluster"`
			Node    bool `json:"node" yaml:"node"`
			Bucket  bool `json:"bucket" yaml:"bucket"`
			XDCR    bool `json:"xdcr" yaml:"xdcr"`
		} `json:"scrape" yaml:"scrape"`
	}

	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err.Error())
	}
	exPath := filepath.Dir(ex)

	var conf confStruct
	if _, err := os.Stat(exPath + "/config.json"); err == nil {
		rawConf, err := ioutil.ReadFile(exPath + "/config.json")
		if err != nil {
			log.Fatal("Could not read file config.json in directory " + exPath)
		}
		err = json.Unmarshal(rawConf, &conf)
		if err != nil {
			log.Fatal("Could not unmarshal file config.json")
		}
	} else if _, err := os.Stat(exPath + "/config.yml"); err == nil {
		rawConf, err := ioutil.ReadFile(exPath + "/config.yml")
		if err != nil {
			log.Fatal("Could not read file config.yaml" + exPath)
		}
		err = yaml.Unmarshal(rawConf, &conf)
		if err != nil {
			log.Fatal("Could not unmarshal file config.yaml")
		}
	} else {
		log.Info("No configuration file was found in the working directory ", exPath, ". Working with command-line arguments and/or environment variables")
		return
	}

	*listenAddr = conf.Web.ListenAddress
	*metricsPath = conf.Web.TelemetryPath
	*dbURI = conf.DB.URI
	dbUser = conf.DB.User
	dbPwd = conf.DB.Password
	*logLevel = conf.Log.Level
	*logFormat = conf.Log.Format
	*scrapeCluster = conf.Scrape.Cluster
	*scrapeNode = conf.Scrape.Node
	*scrapeBucket = conf.Scrape.Bucket
	*scrapeXDCR = conf.Scrape.XDCR
}

func checkCredentials() {
	if len(dbUser) == 0 || len(dbPwd) == 0 {
		log.Fatal("Couchbase username and/or password are not set. You can set them either by providing a configuration file, or with environment variables")
	}
}

func setLogLevel() log.Level {
	var level log.Level
	switch *logLevel {
	case "debug":
		level = log.DebugLevel
	case "warn":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	case "fatal":
		level = log.FatalLevel
	default:
		level = log.InfoLevel
	}
	return level
}

func setLogFormat() log.Formatter {
	var format log.Formatter
	switch *logFormat {
	case "json":
		format = &log.JSONFormatter{}
	default:
		format = &log.TextFormatter{}
	}
	return format
}

func systemdSettings() {
	d.SdNotify(false, "READY=1")
	go func() {
		interval, err := d.SdWatchdogEnabled(false)
		if err != nil || interval == 0 {
			return
		}
		for {
			_, err := http.Get(*dbURI)
			if err == nil {
				d.SdNotify(false, "WATCHDOG=1")
			}
			time.Sleep(interval / 3)
		}
	}()
}

func getCouchbaseVersion(context *collector.Context) {
	body, err := collector.Fetch(*context, "/pools")
	if err != nil {
		log.Error("Error when retrieving Couchbase version informations")
		return
	}
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		log.Error("Could not parse Couchbase version infos")
		return
	}

	rawVersion := data["implementationVersion"].(string)

	log.Info("Couchbase version ", rawVersion)

	for _, v := range validVersions {
		if strings.HasPrefix(rawVersion, v) {
			context.CouchbaseVersion = v
			return
		}
	}

	log.Warn("Version ", rawVersion, " may not be supported by this exporter")
}

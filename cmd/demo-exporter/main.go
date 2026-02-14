package main

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Cardinality dimensions — enough to stress the slow dashboard, low enough for a laptop.
var (
	jobs       = []string{"api-server", "node-exporter", "prometheus"}
	namespaces = []string{"default", "monitoring", "kube-system"}
	statuses   = []string{"200", "201", "404", "500", "503", "error"}
	modes      = []string{"user", "system", "iowait", "idle"}
	cpus       = []string{"0", "1", "2", "3"}
	devices    = []string{"sda", "sdb", "nvme0n1"}
	netDevices = []string{"eth0", "eth1", "lo"}
	mounts     = []string{"/", "/var", "/data"}
)

func instances(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("instance-%03d:9090", i)
	}
	return out
}

func pods(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("pod-%d", i)
	}
	return out
}

func main() {
	reg := prometheus.NewRegistry()
	insts := instances(2)
	podNames := pods(10)

	// --- HTTP application metrics ---
	httpRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests.",
	}, []string{"job", "namespace", "status", "instance", "pod"})
	reg.MustRegister(httpRequests)

	httpDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"job", "namespace", "pod", "container", "instance", "le_group"})
	reg.MustRegister(httpDuration)

	// --- Node metrics ---
	nodeCPU := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "node_cpu_seconds_total",
		Help: "CPU time by mode.",
	}, []string{"instance", "mode", "cpu"})
	reg.MustRegister(nodeCPU)

	nodeMemTotal := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_memory_MemTotal_bytes",
		Help: "Total memory.",
	}, []string{"instance"})
	reg.MustRegister(nodeMemTotal)

	nodeMemFree := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_memory_MemFree_bytes",
		Help: "Free memory.",
	}, []string{"instance"})
	reg.MustRegister(nodeMemFree)

	nodeFSAvail := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_filesystem_avail_bytes",
		Help: "Available filesystem space.",
	}, []string{"instance", "mountpoint", "device"})
	reg.MustRegister(nodeFSAvail)

	nodeFSSize := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_filesystem_size_bytes",
		Help: "Total filesystem size.",
	}, []string{"instance", "mountpoint", "device"})
	reg.MustRegister(nodeFSSize)

	nodeDiskRead := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "node_disk_read_bytes_total",
		Help: "Disk bytes read.",
	}, []string{"instance", "device"})
	reg.MustRegister(nodeDiskRead)

	nodeDiskWrite := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "node_disk_written_bytes_total",
		Help: "Disk bytes written.",
	}, []string{"instance", "device"})
	reg.MustRegister(nodeDiskWrite)

	nodeNetRx := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "node_network_receive_bytes_total",
		Help: "Network bytes received.",
	}, []string{"instance", "device"})
	reg.MustRegister(nodeNetRx)

	nodeNetTx := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "node_network_transmit_bytes_total",
		Help: "Network bytes transmitted.",
	}, []string{"instance", "device"})
	reg.MustRegister(nodeNetTx)

	nodeLoad1 := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_load1", Help: "1-minute load average.",
	}, []string{"instance"})
	reg.MustRegister(nodeLoad1)

	nodeLoad5 := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_load5", Help: "5-minute load average.",
	}, []string{"instance"})
	reg.MustRegister(nodeLoad5)

	nodeLoad15 := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "node_load15", Help: "15-minute load average.",
	}, []string{"instance"})
	reg.MustRegister(nodeLoad15)

	// --- Go / process metrics ---
	goGoroutines := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "go_goroutines", Help: "Number of goroutines.",
	}, []string{"job"})
	reg.MustRegister(goGoroutines)

	processMemory := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "process_resident_memory_bytes", Help: "Resident memory size.",
	}, []string{"job"})
	reg.MustRegister(processMemory)

	// --- Prometheus internal metrics ---
	tsdbHeadSeries := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "prometheus_tsdb_head_series", Help: "Head series count.",
	})
	reg.MustRegister(tsdbHeadSeries)

	tsdbCompactions := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prometheus_tsdb_compactions_total", Help: "TSDB compactions.",
	})
	reg.MustRegister(tsdbCompactions)

	queryDuration := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "prometheus_engine_query_duration_seconds_sum", Help: "Query engine time.",
	})
	reg.MustRegister(queryDuration)

	// --- Kubernetes info ---
	kubePodInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kube_pod_info", Help: "Kubernetes pod information.",
	}, []string{"pod", "namespace", "instance"})
	reg.MustRegister(kubePodInfo)

	// --- up metric ---
	upGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "up", Help: "Target is up.",
	}, []string{"job", "instance", "namespace", "pod"})
	reg.MustRegister(upGauge)

	// Initialize static values
	for _, inst := range insts {
		nodeMemTotal.WithLabelValues(inst).Set(16e9)
		nodeMemFree.WithLabelValues(inst).Set(8e9 + rand.Float64()*4e9)

		for _, mp := range mounts {
			for _, dev := range devices {
				nodeFSSize.WithLabelValues(inst, mp, dev).Set(500e9)
				nodeFSAvail.WithLabelValues(inst, mp, dev).Set(200e9 + rand.Float64()*200e9)
			}
		}
	}

	for _, pod := range podNames {
		for _, ns := range namespaces {
			kubePodInfo.WithLabelValues(pod, ns, insts[0]).Set(1)
		}
	}

	for _, job := range jobs {
		for _, inst := range insts {
			for _, ns := range namespaces {
				for _, pod := range podNames {
					upGauge.WithLabelValues(job, inst, ns, pod).Set(1)
				}
			}
		}
	}

	// Background incrementor
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		tick := 0
		for range ticker.C {
			tick++

			// HTTP requests — vary rate by status
			for _, job := range jobs[:2] { // only api-server and node-exporter
				for _, ns := range namespaces {
					for _, inst := range insts {
						for _, pod := range podNames {
							for _, status := range statuses {
								rate := 1.0
								if status == "200" {
									rate = 5.0 + rand.Float64()*3
								} else if status == "500" || status == "503" {
									rate = 0.1 + rand.Float64()*0.2
								}
								httpRequests.WithLabelValues(job, ns, status, inst, pod).Add(rate)
							}
						}
					}
				}
			}

			// HTTP latency observations
			for _, job := range jobs[:2] {
				for _, ns := range namespaces {
					for _, pod := range podNames[:3] {
						latency := 0.05 + rand.Float64()*0.1
						httpDuration.WithLabelValues(job, ns, pod, "main", insts[0], "default").Observe(latency)
					}
				}
			}

			// CPU time — monotonically increasing
			for _, inst := range insts {
				for _, cpu := range cpus {
					for _, mode := range modes {
						inc := 0.25
						if mode == "idle" {
							inc = 0.75
						}
						inc += rand.Float64() * 0.1
						nodeCPU.WithLabelValues(inst, mode, cpu).Add(inc)
					}
				}
			}

			// Disk I/O
			for _, inst := range insts {
				for _, dev := range devices {
					nodeDiskRead.WithLabelValues(inst, dev).Add(float64(rand.Intn(1e6)))
					nodeDiskWrite.WithLabelValues(inst, dev).Add(float64(rand.Intn(5e5)))
				}
			}

			// Network I/O
			for _, inst := range insts {
				for _, dev := range netDevices {
					nodeNetRx.WithLabelValues(inst, dev).Add(float64(rand.Intn(1e7)))
					nodeNetTx.WithLabelValues(inst, dev).Add(float64(rand.Intn(5e6)))
				}
			}

			// Load averages — sinusoidal with noise
			for _, inst := range insts {
				base := 2.0 + math.Sin(float64(tick)/60.0)*1.5
				nodeLoad1.WithLabelValues(inst).Set(base + rand.Float64()*0.5)
				nodeLoad5.WithLabelValues(inst).Set(base + rand.Float64()*0.3)
				nodeLoad15.WithLabelValues(inst).Set(base + rand.Float64()*0.1)
			}

			// Memory fluctuation
			for _, inst := range insts {
				nodeMemFree.WithLabelValues(inst).Set(4e9 + rand.Float64()*8e9)
			}

			// Goroutines and process memory
			for _, job := range jobs {
				goGoroutines.WithLabelValues(job).Set(50 + rand.Float64()*200)
				processMemory.WithLabelValues(job).Set(100e6 + rand.Float64()*400e6)
			}

			// Prometheus internal
			tsdbHeadSeries.Set(5000 + rand.Float64()*500)
			if tick%10 == 0 {
				tsdbCompactions.Add(1)
			}
			queryDuration.Add(0.01 + rand.Float64()*0.05)
		}
	}()

	log.Println("Synthetic exporter listening on :9099")
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	log.Fatal(http.ListenAndServe(":9099", nil))
}

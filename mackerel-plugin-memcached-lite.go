package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"github.com/jessevdk/go-flags"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

type connectionOpts struct {
	Host    string  `short:"H" long:"host" default:"localhost" description:"Hostname"`
	Port    string  `short:"p" long:"port" default:"11211" description:"Port"`
	Timeout float64 `short:"t" long:"timeout" default:"10" description:"Seconds before connection times out"`
}

func write(conn net.Conn, content []byte, timeout float64) error {
	if timeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	}
	_, err := conn.Write(content)
	return err
}

func slurp(conn net.Conn, timeout float64) ([]byte, error) {
	buf := []byte{}
	readLimit := 32 * 1024
	if timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	}
	for {
		tmpBuf := make([]byte, readLimit)
		i, err := conn.Read(tmpBuf)
		if i > 0 {
			buf = append(buf, tmpBuf[:i]...)
		}
		if err == io.EOF || i < readLimit {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
	return buf, nil
}

func fetchStats(conn net.Conn, stats map[string]int64, timeout float64) error {
	buf, err := slurp(conn, timeout)
	if err != nil {
		return err
	}

	for _, b := range bytes.Split(buf, []byte("\n")) {
		if match := regexp.MustCompile(`^STAT ([a-z_]+) (\d+)`).FindStringSubmatch(string(b)); match != nil {
			i, err := strconv.ParseInt(match[2], 0, 64)
			if err != nil {
				return err
			}
			// fmt.Fprintf(os.Stderr, "%s==%s\n",match[1],match[2]);
			stats[match[1]] = i
		}
	}
	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func writeStats(filename string, stats map[string]int64) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	for k, v := range stats {
		file.WriteString(fmt.Sprintf("%s\t%d\n", k, v))
	}

	return nil
}

func loadStats(filename string, stats map[string]int64) error {
	fmt.Fprintf(os.Stderr, filename)
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.LazyQuotes = true // ダブルクオートを厳密にチェックしない！
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		i, err := strconv.ParseInt(record[1], 0, 64)
		if err != nil {
			return err
		}
		stats[record[0]] = i
	}
	return nil
}

func memcachedStats(opts connectionOpts) (st int) {
	address := fmt.Sprintf("%s:%s", opts.Host, opts.Port)

	conn, err := net.Dial("tcp", address)
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't connect to memcached: %s\n", err.Error())
		return
	}
	defer conn.Close()

	stats := make(map[string]int64)
	err = write(conn, []byte("stats\r\n"), opts.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to send command: %s\n", err.Error())
		return
	}
	err = fetchStats(conn, stats, opts.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch stats: %s\n", err.Error())
		return
	}

	err = write(conn, []byte("stats settings\r\n"), opts.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to send command: %s\n", err.Error())
		return
	}
	err = fetchStats(conn, stats, opts.Timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch stats settings: %s\n", err.Error())
		return
	}

	tmpDir := os.TempDir()
	curUser, _ := user.Current()
	prevPath := filepath.Join(tmpDir, curUser.Uid+fmt.Sprintf("-mackerel-plugin-memcached-lite-%s-%s", opts.Host, opts.Port))
	now := int64(time.Now().Unix())
	stats["_time_"] = now

	if !fileExists(prevPath) {
		err = writeStats(prevPath, stats)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to save stats: %s\n", err.Error())
			return
		}
		st = 1
		fmt.Fprintf(os.Stderr, "Notice: first time execution command\n")
		return
	}

	prev := make(map[string]int64)
	err = loadStats(prevPath, prev)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load previous stats: %s\n", err.Error())
		return
	}

	period := stats["_time_"] - prev["_time_"]
	fmt.Fprintf(os.Stderr, "[debug] %d\n", period)

	// cache usage
	fmt.Printf("memcached-lite.cache-usage-byte.used\t%d\t%d\n", stats["bytes"], now)
	fmt.Printf("memcached-lite.cache-usage-byte.max\t%d\t%d\n", stats["limit_maxbytes"], now)

	// cache items
	fmt.Printf("memcached-lite.cache-items.current\t%d\t%d\n", stats["curr_items"], now)

	// evictions
	// request
	m := map[string]string{"req-per-sec.get": "cmd_get", "req-per-sec.set": "cmd_set", "eviction-per-sec.total": "evictions", "eviction-per-sec.unfetched": "evicted_unfetched"}
	for key, skey := range m {
		//fmt.Fprintf(os.Stderr, "[debug] %s = %d - %d / %d\n", skey, stats[skey], prev[skey], period)
		gap := stats[skey] - prev[skey]
		if gap < 0 {
			gap = stats[skey]
		}
		//fmt.Fprintf(os.Stderr, "[debug] %s = %d - %d | %d | %d\n", skey, stats[skey], prev[skey], gap, gap/period)
		fmt.Printf("memcached-lite.%s\t%f\t%d\n", key, float64(gap)/float64(period), now)
	}

	// cache hit rate
	hits := stats["get_hits"] - prev["get_hits"]
	if hits < 0 {
		hits = stats["get_hits"]
	}
	misses := stats["get_misses"] - prev["get_misses"]
	if misses < 0 {
		misses = stats["get_misses"]
	}

	if hits+misses <= 0 {
		fmt.Printf("memcached-lite.cache-hit.rate\t%f\t%d\n", float64(0), now)
	} else {
		fmt.Printf("memcached-lite.cache-hit.rate\t%f\t%d\n", float64(hits*100)/float64(hits+misses), now)
	}

	// connections
	fmt.Printf("memcached-lite.connections.current\t%d\t%d\n", stats["curr_connections"], now)
	fmt.Printf("memcached-lite.connections.max\t%d\t%d\n", stats["maxconns"], now)

	err = writeStats(prevPath, stats)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to save stats: %s\n", err.Error())
		return
	}

	st = 0
	return
}

func memcachedDef(opts connectionOpts) (st int) {
	st = 0
	// # mackerel-agent-plugin
	str := `
{
  "graphs": {
    "memcached-lite.cache-usage-byte": {
      "label": "memcached-lite cache usage byte",
      "unit": "integer",
      "metrics": [
        { "name": "used", "label": "Used" },
        { "name": "max", "label": "Max" }
      ]
    },
    "memcached-lite.cache-items": {
      "label": "memcached-lite cache items",
      "unit": "integer",
      "metrics": [
        { "name": "current", "label": "Used", "stacked": true }
      ]
    },
    "memcached-lite.eviction-per-sec": {
      "label": "memcached-lite evicted items per sec",
      "unit": "float",
      "metrics": [
        { "name": "total", "label": "Total" },
        { "name": "unfetched", "label": "Unfetched" }
      ]
    },
    "memcached-lite.req-per-sec": {
      "label": "memcached-lite request per sec",
      "unit": "float",
      "metrics": [
        { "name": "get", "label": "Get" },
        { "name": "set", "label": "Set", "stacked": true }
      ]
    },
    "memcached-lite.cache-hit": {
      "label": "memcached-lite cache hit rate",
      "unit": "float",
      "metrics": [
        { "name": "rate", "label": "Rate", "stacked": true }
      ]
    },
    "memcached-lite.connections": {
      "label": "memcached-lite connections",
      "unit": "integer",
      "metrics": [
        { "name": "current", "label": "Current" },
        { "name": "max", "label": "Max" }
      ]
    }
  }
}
`
	fmt.Printf(str);
	return
}

func main() {
	os.Exit(_main())
}

func _main() (st int) {
	opts := connectionOpts{}
	psr := flags.NewParser(&opts, flags.Default)
	_, err := psr.Parse()
	if err != nil {
		os.Exit(1)
	}

	if os.Getenv("MACKEREL_AGENT_PLUGIN_META") != "" {
		st = memcachedDef(opts)
	} else {
		st = memcachedStats(opts)
	}
	return
}

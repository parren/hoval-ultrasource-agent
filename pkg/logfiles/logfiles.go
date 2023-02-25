// Package logfiles implements logging to CSV files by day and header hash.
package logfiles

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"hash/fnv"
	"os"
	"time"
)

type LogFileStore struct {
	Dir string
}

func FormatTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func (ls LogFileStore) Write(t time.Time, header, row []interface{}) error {
	hs := encode(header)
	hid, err := hash(hs)
	if err != nil {
		return err
	}
	dn := fmt.Sprintf("%s/%v", ls.Dir, hid)
	if err := os.Mkdir(dn, 0777); err != nil {
		if !os.IsExist(err) {
			return fmt.Errorf("failed to create %s: %v", dn, err)
		}
	} else {
		if err := write(hs, dn+"/header.csv", os.O_EXCL); err != nil {
			return err
		}
	}
	fn := fmt.Sprintf("%s/%04d-%02d-%02d.csv", dn, t.Year(), t.Month(), t.Day())
	return write(encode(row), fn, os.O_APPEND)
}

func encode(vs []interface{}) string {
	ss := make([]string, len(vs))
	for i, v := range vs {
		ss[i] = fmt.Sprintf("%v", v)
	}
	b := bytes.Buffer{}
	w := csv.NewWriter(&b)
	w.Write(ss)
	w.Flush()
	return b.String()
}

func hash(s string) (uint64, error) {
	h := fnv.New64()
	if _, err := h.Write([]byte(s)); err != nil {
		return 0, err
	}
	return h.Sum64(), nil
}

func write(s string, fn string, mode int) error {
	f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE|mode, 0666)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(s)
	return err
}

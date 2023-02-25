package logfiles

import (
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/karlseguin/expect"
)

type Tests struct{}

func Test_LogFiles(t *testing.T) {
	Expectify(new(Tests), t)
}

func (Tests) WritesCorrectTree() {
	d := logDir()
	defer os.RemoveAll(d)

	ls := LogFileStore{Dir: d}

	t1 := time.Date(2023, 3, 30, 8, 10, 20, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t1.Add(time.Hour * 24)
	t4 := t3.Add(time.Hour)

	h1 := []interface{}{"Time", "Temp"}
	h2 := []interface{}{"Time", "Prog", "Temp"}

	Expect(ls.Write(t1, h1, []interface{}{t1, 12.34})).ToEqual(nil)
	v1 := "2942268501709470014"
	Expect(readFile(fmt.Sprintf("%s/%s/header.csv", d, v1))).
		ToEqual("Time,Temp\n", nil)
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-30.csv", d, v1))).
		ToEqual("2023-03-30 08:10:20 +0000 UTC,12.34\n", nil)

	Expect(ls.Write(t2, h1, []interface{}{t2, 23.45})).ToEqual(nil)
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-30.csv", d, v1))).
		ToEqual("2023-03-30 08:10:20 +0000 UTC,12.34\n"+
			"2023-03-30 09:10:20 +0000 UTC,23.45\n", nil)

	Expect(ls.Write(t3, h1, []interface{}{t3, -12.34})).ToEqual(nil)
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-30.csv", d, v1))).
		ToEqual("2023-03-30 08:10:20 +0000 UTC,12.34\n"+
			"2023-03-30 09:10:20 +0000 UTC,23.45\n", nil)
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-31.csv", d, v1))).
		ToEqual("2023-03-31 08:10:20 +0000 UTC,-12.34\n", nil)

	Expect(ls.Write(t4, h2, []interface{}{t4, "default", 12})).ToEqual(nil)
	v2 := "8611046381969870286"
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-31.csv", d, v1))).
		ToEqual("2023-03-31 08:10:20 +0000 UTC,-12.34\n", nil)
	Expect(readFile(fmt.Sprintf("%s/%s/header.csv", d, v2))).
		ToEqual("Time,Prog,Temp\n", nil)
	Expect(readFile(fmt.Sprintf("%s/%s/2023-03-31.csv", d, v2))).
		ToEqual("2023-03-31 09:10:20 +0000 UTC,default,12\n", nil)
}

func logDir() string {
	d, err := os.MkdirTemp("/tmp", "parren-ch_logfiles_test")
	if err != nil {
		panic(err)
	}
	return d
}

func readFile(fn string) (string, error) {
	b, err := os.ReadFile(fn)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

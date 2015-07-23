package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"github.com/tildeleb/hashland/jenkins"
)

type kfe struct {
	path string
	size int64
	hash uint64
}

var hmap = make(map[uint64][]kfe, 100)
var smap = make(map[int64][]kfe, 100)

func fullName(path string, fi os.FileInfo) string {
	p := ""
	if path == "" {
		p = fi.Name()
	} else {
		p = path + "/" + fi.Name()		
	}
	return p
}


func readPartialHash(path string, fi os.FileInfo) uint64 {
	h := jenkins.New364(0)
	buf := make([]byte, 8192)
	p := fullName(path, fi)
	f, err := os.Open(p)
	if err != nil {
		panic("hashFile")
	}
	l := 0
	if fi.Size() <= 8192 {
		l, _ = f.Read(buf)
	} else {
		l, _ = f.Read(buf[0:4096])
	    _, _ = f.Seek(-4096, os.SEEK_END)
		l2, _ := f.Read(buf[4096:])
		l += l2
		if l != 8192 {
			panic("8192")
		}
	}
	f.Close()
	h.Write(buf[0:l])
	r := h.Sum64()
	//fmt.Printf("file=%q, hash=0x%016x\n", p, r)
	return r
}

func addFile(path string, fi os.FileInfo) {
	p := fullName(path, fi)
	k1 := kfe{p, fi.Size(), 0}

	skey := fi.Size()
	// 0 length files are currently silently ignored
	// they are not identical
	if (skey > 0) {
		hkey := uint64(readPartialHash(path, fi))
		_, ok := hmap[hkey]
		if !ok {
			hmap[hkey] = []kfe{k1}
		} else {
			hmap[hkey] = append(hmap[hkey], k1)
		}

		_, ok2 := smap[skey]
		if !ok2 {
			smap[skey] = []kfe{k1}
		} else {
			smap[skey] = append(smap[skey], k1)
		}
	}
}

func addDirs(path string, fis []os.FileInfo) {
	//fmt.Printf("addDirs: path=%q\n", path)
	for _, fi := range fis {
		//fmt.Printf("addDirs: fi.Name=%q\n", fi.Name())
		switch {
		case fi.Mode()&os.ModeDir == os.ModeDir:
			p := fullName(path, fi)
			//fmt.Printf("addDirs: dir=%q\n", p)
			d, err := os.Open(p)
			if err != nil {
				continue
			}
			fis, err := d.Readdir(-1)
			if err != nil || fis == nil {
				panic("bad")
			}
			d.Close()
			addDirs(p, fis)
		case fi.Mode()&os.ModeType == 0:
			//fmt.Printf("addFile: path=%q, fi.Name()=%q\n", path, fi.Name())
			addFile(path, fi)
		default:
			continue
		}
	}
}

func main() {
	fmt.Printf("dedup\n")
	flag.Parse()
	if len(flag.Args()) != 0 {
		for _, dir := range flag.Args() {
			fi, err := os.Stat(dir)
			if err != nil || fi == nil {
				panic("bad")
			}
			path := ""
			idx := strings.LastIndex(dir, "/")
			if idx != -1 {
				path = dir[0:idx]
			}
			fis := []os.FileInfo{fi}
			addDirs(path, fis)
//			dirs := []string{dir}
		}
	}
/*
	for k, v := range smap {
		if len(v) > 1 {
			fmt.Printf("%d\n", k)
			for _, v2 := range v {
				fmt.Printf("\t%q\n", v2.path)
			}
		}
	}
*/
	for k, v := range hmap {
		if len(v) > 1 {
			fmt.Printf("0x%016x\n", k)
			for _, v2 := range v {
				fmt.Printf("\t%q\n", v2.path)
			}
		}
	}
}
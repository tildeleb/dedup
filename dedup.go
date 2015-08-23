package main

import (
	"flag"
	"fmt"
	"github.com/tildeleb/hashland/aeshash"
	_ "github.com/tildeleb/hashland/jenkins"
	"leb.io/hrff"
	"log"
	"os"
	"regexp"
	"strings"
)

type kfe struct {
	path string
	size int64
	hash uint64
}

type stat struct {
	scannedFiles int64
	scannedDirs  int64
	matchedFiles int64
	matchedDirs  int64
}

var stats stat

var ddre *regexp.Regexp
var patre *regexp.Regexp

var blockSize = flag.Int64("b", 8192, "block size")
var dirf = flag.Bool("d", false, "hash dirs")
var pat = flag.String("pat", "", "regexp pattern to match filenames")
var dd = flag.String("dd", "", "do not descend past dirs named this")
var print = flag.Bool("p", false, "print duplicated dirs or files")

var _threshold hrff.Int64
var threshold int64
var total int64
var count int64
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
	p := fullName(path, fi)
	//fmt.Printf("readPartialHash: path=%q fi.Name=%q, p=%q\n", path, fi.Name(), p)
	if fi.Size() == 0 {
		return 0
	}
	//h := jenkins.New364(0)
	var half = *blockSize / 2
	buf := make([]byte, *blockSize)

	f, err := os.Open(p)
	if err != nil {
		panic("readPartialHash: Open")
	}
	l := 0
	if fi.Size() <= *blockSize {
		l, _ = f.Read(buf)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		l, err = f.Read(buf[0:half])
		if err != nil {
			log.Fatal(err)
		}
		_, _ = f.Seek(-half, os.SEEK_END)
		l2, _ := f.Read(buf[half:])
		if err != nil {
			log.Fatal(err)
		}
		lt := l + l2
		if lt != int(*blockSize) {
			fmt.Printf("blocksSize=%d, half=%d\n", *blockSize, half)
			fmt.Printf("f=%q, size=%d, l=%d, l2=%d, lt=%d\n", fi.Name(), fi.Size(), l, l2, lt)
			panic("readPartialHash: blockSize")
		}
	}
	f.Close()
	r := uint64(0)
	r = aeshash.Hash(buf[0:l], 0)
	//h.Write(buf[0:l])
	//r = h.Sum64()
	//fmt.Printf("file=%q, hash=0x%016x\n", p, r)
	return r
}

func add(hash uint64, size int64, k *kfe) {
	_, ok := hmap[hash]
	if !ok {
		hmap[hash] = []kfe{*k}
	} else {
		hmap[hash] = append(hmap[hash], *k)
	}
}

func addFile(path string, fi os.FileInfo, hash uint64, size int64) {
	p := fullName(path, fi)
	//fmt.Printf("addFile: path=%q, fi.Name()=%q, p=%q\n", path, fi.Name(), p)
	k1 := kfe{p, fi.Size(), 0}

	skey := fi.Size()
	// 0 length files are currently silently ignored
	// they are not identical
	if skey > threshold {
		hkey := uint64(readPartialHash(path, fi))
		add(hkey, skey, &k1)
		/*
			_, ok := hmap[hkey]
			if !ok {
				hmap[hkey] = []kfe{k1}
			} else {
				hmap[hkey] = append(hmap[hkey], k1)
			}
		*/
		_, ok2 := smap[skey]
		if !ok2 {
			smap[skey] = []kfe{k1}
		} else {
			smap[skey] = append(smap[skey], k1)
		}
	}
}

func addDir(path string, fi os.FileInfo, hash uint64, size int64) {
	p := fullName(path, fi)
	//fmt.Printf("addDir: path=%q, fi.Name()=%q, p=%q, size=%d, hash=0x%016x\n", path, fi.Name(), p, size, hash)
	k1 := kfe{p, size, hash}

	_, ok := hmap[hash]
	if !ok {
		hmap[hash] = []kfe{k1}
	} else {
		hmap[hash] = append(hmap[hash], k1)
	}
}

/*
func hashDir(path string, fi os.FileInfo) {
	p := fullName(path, fi)

	//fmt.Printf("addDirs: dir=%q\n", p)
	d, err := os.Open(p)
	if err != nil {
		continue
	}
	fis, err := d.Readdir(-1)
	if err != nil || fis == nil {
		fmt.Printf("addDirs: can't read %q\n", p)
		continue
	}
	d.Close()
	addDirs(p, fis)
}


func addDirs(path string, fis []os.FileInfo) {
	//fmt.Printf("addDirs: path=%q\n", path)
	for _, fi := range fis {
		//fmt.Printf("addDirs: fi.Name=%q\n", fi.Name())
		switch {
		case fi.Mode()&os.ModeDir == os.ModeDir:
			stats.scannedDirs++
			if *dd != "" {
				b := ddre.MatchString(fi.Name())
				if b {
					//fmt.Printf("addDirs: skip dir=%q\n", fi.Name())
					continue
				}
			}
			p := fullName(path, fi)
			//fmt.Printf("addDirs: dir=%q\n", p)
			d, err := os.Open(p)
			if err != nil {
				continue
			}
			fis, err := d.Readdir(-1)
			if err != nil || fis == nil {
				fmt.Printf("addDirs: can't read %q\n", fullName(path, fi))
				continue
			}
			d.Close()
			addDirs(p, fis)
		case fi.Mode()&os.ModeType == 0:
			stats.scannedFiles++
			//fmt.Printf("addFile: path=%q, fi.Name()=%q\n", path, fi.Name())
			addFile(path, fi, 0, 0)
		default:
			continue
		}
	}
}

var sortMap = make([]*kfe, 0)

func addDir(path string, fi os.FileInfo) (uint64, int64) {
	var gh = aeshash.NewAES(0)
	var hash uint64
	var size, sizes int64
	var cnt int

	//fmt.Printf("addDir: path=%q\n", path)
	if fi.Mode()&os.ModeDir == os.ModeDir {
		d, err := os.Open(path)
		if err != nil {
			return 0, 0
			fmt.Printf("addDir: path=%q\n", path)
			panic("Open")
		}
		fis, err := d.Readdir(-1)
		if err != nil || fis == nil {
			//fmt.Printf("addDirs: can't read %q\n", fullName(path, fi))
			panic("Readdir")
		}
		d.Close()
		for _, fi := range fis {
			p := fullName(path, fi)
			//fmt.Printf("addDir: fi.Name=%q\n", fi.Name())
			switch {
			case fi.Mode()&os.ModeDir == os.ModeDir:
				hash, size = addDir(p, fi) // wrong
				//fmt.Printf("addDir: dir=%q, hash=0x%016x, size=%d\n", p, hash, size)
				cnt++
			case fi.Mode()&os.ModeType == 0:
				if fi.Size() > threshold {
					hash = readPartialHash(path, fi)
					cnt++
					size += fi.Size()
				}
				//fmt.Printf("addDir: file=%q, hash=0x%016x\n", p, hash)
			default:
				continue
			}
			gh.Write64(hash)
			sizes += size
		}
	}
	hashes := gh.Sum64()
	//fmt.Printf("addDir: path=%q hash=0x%016x, size=%d\n", path, hashes, sizes)

	if cnt > 0 {
		k1 := kfe{path, sizes, hashes}
		sortMap = append(sortMap, &k1)
		_, ok := hmap[hashes]
		if !ok {
			hmap[hashes] = []kfe{k1}
		} else {
			hmap[hashes] = append(hmap[hashes], k1)
		}
		return hashes, sizes
	}
	return 0, 0
}
*/

func descend(path string, fis []os.FileInfo,
	ffp func(path string, fis os.FileInfo, hash uint64, size int64),
	dfp func(path string, fis os.FileInfo, hash uint64, size int64)) (uint64, int64) {
	var hash uint64
	var size, sizes int64

	var des func(path string, fis []os.FileInfo) (uint64, int64)
	des = func(path string, fis []os.FileInfo) (uint64, int64) {
		var gh = aeshash.NewAES(0)

		for _, fi := range fis {
			//fmt.Printf("des: fi.Name=%q\n", fi.Name())
			switch {
			case fi.Mode()&os.ModeDir == os.ModeDir:
				stats.scannedDirs++
				if *dd != "" {
					b := ddre.MatchString(fi.Name())
					if b {
						fmt.Printf("des: skip dir=%q\n", fi.Name())
						continue
					}
				}
				p := fullName(path, fi)
				//fmt.Printf("des: dir=%q\n", p)
				d, err := os.Open(p)
				if err != nil {
					continue
				}
				fis, err := d.Readdir(-1)
				if err != nil || fis == nil {
					fmt.Printf("des: can't read %q\n", fullName(path, fi))
					continue
				}
				d.Close()
				h, size := des(p, fis)
				hash = h
				gh.Write64(hash)
				sizes += size
				stats.matchedDirs++
				if dfp != nil {
					dfp(path, fi, hash, size)
				}
			case fi.Mode()&os.ModeType == 0:
				//fmt.Printf("des: addFile: path=%q, fi.Name()=%q\n", path, fi.Name())
				stats.scannedFiles++
				sizes += fi.Size()
				if fi.Size() > threshold && (*pat == "" || (*pat != "" && patre.MatchString(fi.Name()))) {
					hash = readPartialHash(path, fi)
					gh.Write64(hash)
					stats.matchedFiles++
					if ffp != nil {
						ffp(path, fi, hash, size)
					}
				}
			default:
				continue
			}
		}
		hashes := gh.Sum64()
		return hashes, sizes
	}
	//fmt.Printf("des: path=%q\n", path)
	return des(path, fis)
}

/*
func dirs(path string, fi os.FileInfo) {
	//var gh = aeshash.NewAES(0)
	var hash uint64
	var size int64 // sizes
	var cnt int

	var addFile = func(path string, fi os.FileInfo) {
		if fi.Size() > threshold {
			hash = readPartialHash(path, fi)
			cnt++
			size += fi.Size()
		}
	}

	//descend(path, fis, addFile, nil)
	p := fullName(path, fi)
	//fmt.Printf("addFile: path=%q, fi.Name()=%q, p=%q\n", path, fi.Name(), p)
	k1 := kfe{p, fi.Size(), 0}
}
*/

func main() {
	var hash uint64
	var size int64
	var kind = ""

	flag.Var(&_threshold, "t", "threshhold")
	//fmt.Printf("dedup\n")
	flag.Parse()
	if *pat != "" {
		re, err := regexp.Compile(*pat)
		if err != nil {
			return
		}
		patre = re
	}
	if *dd != "" {
		re, err := regexp.Compile(*dd)
		if err != nil {
			return
		}
		ddre = re
	}
	threshold = int64(_threshold.V)
	//fmt.Printf("threshold=%d\n", threshold)
	if len(flag.Args()) != 0 {
		for _, dir := range flag.Args() {
			fi, err := os.Stat(dir)
			if err != nil || fi == nil {
				fmt.Printf("fi=%#v, err=%v\n", fi, err)
				panic("bad")
			}
			path := ""
			switch {
			case fi.Mode()&os.ModeDir == os.ModeDir:
				idx := strings.LastIndex(dir, "/")
				if idx != -1 {
					path = dir[0:idx]
				}
				fis := []os.FileInfo{fi}
				if *dirf {
					//hash, size = addDir(dir, fi)
					hash, size = descend(path, fis, nil, addDir)
					add(hash, size, &kfe{path, size, hash})
					kind = "dirs"
				} else {
					//addDirs(path, fis)
					hash, size = descend(path, fis, addFile, nil)
					kind = "file"
				}
			case fi.Mode()&os.ModeType == 0:
				hash = readPartialHash(path, fi)
				fmt.Printf("0x%016x %q\n", hash, dir)
				//fmt.Printf("addFile: path=%q, fi.Name()=%q\n", path, fi.Name())
			}
			if *dirf {
				fmt.Printf("# dir=%q, hash=0x%016x, files totaling %h\n", dir, hash, hrff.Int64{size, "B"})
			}
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
			if *print {
				fmt.Printf("0x%016x ", k)
			}
			for k2, v2 := range v {
				size := hrff.Int64{v2.size, "B"}
				if k2 == 0 && *print {
					fmt.Printf("%d %h\n", v2.size, size)
				}
				total += v2.size
				count++
				if *print {
					fmt.Printf("\t%q\n", v2.path)
				}
			}
		}
	}
	fmt.Printf("# %d duplicated %s totaling %h\n", count, kind, hrff.Int64{total, "B"})
	fmt.Printf("# %d files, %d dirs scanned\n", stats.scannedFiles, stats.scannedDirs)
}

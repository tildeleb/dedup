// Copyright © 2015-2016 Lawrence E. Bakst. All rights reserved.

// Originaly written on a plane from SFO->EWR on 7-23-15 in about an hour.
// Based on an idea I had been mulling in my mind for years.
//
// dedup scans files or directories and calculates fingerprint hashes for them based on their contents.
//
// Without the -d (directory) switch dedup recursively scans the supplied directories in depth first
// order and records the hash of each file in a map of slices keyed by the hash. After the scan is
// complete, the resulting map is iterated, and if any of the slices have a length of more than 1,
// then the files on that slice are all duplicates of each other.
//
// If -d swicth is supplied the hashes of files are themselves recursively hashed and the resulting
// hashes of each directory (but not the files) are recorded in the map. Again, if the length of any
// slice is more than 1 then the entire directory is duplicated.
//
// If the -r switch is supplied, when the map is scanned, any slices with a length different than
// the number of supplied directores are printed as these represent missing files. This allows
// directories to be easily compared and more than two can easily be compared.
//
// Implementation
//
// The hash used is the asehash from the Go runtime. It's fast and passes smhahser.
// The map of slices is not the most memory efficient representation and at some
// point it proably makes sense to switch to a cuckoo hash table.
//
package main

import (
	"flag"
	"fmt"
	"hash"
	"leb.io/aeshash"
	_ "leb.io/hashland/jenkins"
	"leb.io/hrff"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
)

type PathReader interface {
	PathRead(path string, fi os.FileInfo) (r uint64)
}

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
var h = flag.Bool("h", false, "human readable numbers")
var r = flag.Bool("r", false, "reverse sense; record non duplicated files")
var s = flag.Bool("s", false, "sort duplicate files by size from largest to smallest")
var rs = flag.Bool("rs", false, "reverse sense of sort, smallest to largest")
var fr = flag.Bool("fr", false, "full read; read the entire file")
var pat = flag.String("pat", "", "regexp pattern to match filenames")
var dd = flag.String("dd", "", "do not descend past dirs named this")
var print = flag.Bool("p", false, "print duplicated dirs or files")
var ps = flag.Bool("ps", false, "print summary")
var pd = flag.Bool("pd", false, "print duplicates with -r")
var fp = flag.Uint64("fp", 0, "fingerprint to search for.")

var _fthreshold hrff.Int64
var _dthreshold hrff.Int64
var fthreshold int64
var dthreshold int64

var total int64
var count int64
var hmap = make(map[uint64][]kfe, 100)
var smap = make(map[int64][]kfe, 100)

var ignoreList = []string{".Spotlight-V100", ".fseventsd"}

var hf hash.Hash64

func fullName(path string, fi os.FileInfo) string {
	p := ""
	if path == "" {
		p = fi.Name()
	} else {
		p = path + "/" + fi.Name()
	}
	return p
}

func readFullHash(path string, fi os.FileInfo) (r uint64) {
	p := fullName(path, fi)
	//fmt.Printf("readFullHash: path=%q fi.Name=%q, p=%q\n", path, fi.Name(), p)
	if fi.Size() == 0 {
		return 0
	}
	buf := make([]byte, *blockSize)

	f, err := os.Open(p)
	if err != nil {
		panic("readPartialHash: Open")
	}
	defer f.Close()

	hf.Reset()
	for {
		l, err := f.Read(buf)
		//fmt.Printf("f=%q, err=%v, l=%d, size=%d\n", fi.Name(), err, l, fi.Size())
		if l == 0 {
			break
		}
		if l < 0 || err != nil {
			log.Fatal(err)
			return
		}
		hf.Write(buf[:l])
	}
	if false {
		fmt.Printf("blocksSize=%d\n", *blockSize)
		panic("readFullHash: blockSize")
	}
	r = hf.Sum64()
	//fmt.Printf("readFullHash: p=%q, r=%#016x\n", p, r)
	//h.Write(buf[0:l])
	//r = h.Sum64()
	//fmt.Printf("readFullHash: file=%q, hash=0x%016x\n", p, r)
	return r
}

var readList = []float64{0.0, 0.5, 1.0} // at least 0.0 must be first and 1.0 must be last

func readPartialHash(path string, fi os.FileInfo) (r uint64) {
	var eo float64
	p := fullName(path, fi)
	bs := int(*blockSize)
	bsize := bs * len(readList)

	//fmt.Printf("readPartialHash: path=%q fi.Name=%q, p=%q\n", path, fi.Name(), p)
	if fi.Size() == 0 {
		return 0
	}
	buf := make([]byte, bsize)

	f, err := os.Open(p)
	if err != nil {
		panic("readPartialHash: Open")
	}
	defer f.Close()

	lt := 0
	size := float64(fi.Size())

	if false { // lt != bs
		fmt.Printf("f=%q, size=%d, lt=%d\n", fi.Name(), fi.Size(), lt)
		//panic("readPartialHash: blockSize")
	}

	if fi.Size() <= int64(bs) {
		_, _ = f.Seek(0, os.SEEK_SET)
		l, err := f.Read(buf[0:bs])
		if err != nil {
			log.Fatal(err)
		}
		lt = l
	} else {
		for k, v := range readList {
			if k > 0 {
				bo := v * size
				if eo >= bo {
					//fmt.Printf("%d: overlap size=%f, bs=%d, v=%f, eo=%f, bo=%f\n", k, size, bs, v, eo, bo)
					continue
				}
			}
			eo = readList[k]*size + float64(bs)
			if v == 1.0 {
				if size >= float64(2*bs) {
					//fmt.Printf("%d: [%d: %d]\n", k, k*bs, k*bs+bs) // k, int(size)-bs, int(size))
					_, _ = f.Seek(int64(-bs), os.SEEK_END)
				} else {
					break
				}
			} else {
				//fmt.Printf("%d: [%d: %d]\n", k, k*bs, k*bs+bs)
				_, _ = f.Seek(int64(float64(size)*v), os.SEEK_SET)
			}
			l, _ := f.Read(buf[k*bs : k*bs+bs])
			if err != nil {
				log.Fatal(err)
			}
			if fi.Size() > int64(bs) && l != bs {
				continue
			}
			lt += l
		}
	}

	r = aeshash.Hash(buf[0:lt], 0)
	//h.Write(buf[0:l])
	//r = h.Sum64()
	//fmt.Printf("file=%q, hash=0x%016x\n", p, r)
	return
}

func add(hash uint64, size int64, k *kfe) {
	//fmt.Printf("add: kfe=%v\n", k)
	_, ok := hmap[hash]
	if !ok {
		hmap[hash] = []kfe{*k}
	} else {
		hmap[hash] = append(hmap[hash], *k)
	}
}

func addFile(path string, fi os.FileInfo, hash uint64, size int64) {
	p := fullName(path, fi)
	//fmt.Printf("addFile: path=%q, fi.Name()=%q, hash=%#x, p=%q\n", path, fi.Name(), hash, p)
	//fmt.Printf("addFile: hash=%016x, p=%q\n", hash, p)
	k1 := kfe{p, fi.Size(), 0}

	skey := fi.Size()
	// 0 length files are currently silently ignored
	// they are not identical
	//hkey := uint64(0)
	/*
		if skey > fthreshold {
			if *fr {
				hkey = readFullHash(path, fi)
			} else {
				hkey = readPartialHash(path, fi)
			}
		}
	*/
	add(hash, skey, &k1)
	// smap not used
	_, ok2 := smap[skey]
	if !ok2 {
		smap[skey] = []kfe{k1}
	} else {
		smap[skey] = append(smap[skey], k1)
	}
}

func addDir(path string, fi os.FileInfo, hash uint64, size int64) {
	if size <= dthreshold {
		return // should dirs respect threshold or is it only for files?
	}
	p := fullName(path, fi)
	//fmt.Printf("addDir: path=%q, fi.Name()=%q, p=%q, size=%d, hash=0x%016x\n", path, fi.Name(), p, size, hash)
	k1 := kfe{p, size, hash}
	add(hash, size, &k1)
}

func descend(path string, fis []os.FileInfo,
	ffp func(path string, fis os.FileInfo, hash uint64, size int64),
	dfp func(path string, fis os.FileInfo, hash uint64, size int64)) (uint64, int64) {

	var des func(path string, fis []os.FileInfo) (uint64, int64)
	des = func(path string, fis []os.FileInfo) (uint64, int64) {
		var hash uint64
		var size, sizes int64
		var gh = aeshash.NewAES(0)

	outer:
		for _, fi := range fis {
			//fmt.Printf("descend: enter fi.Name=%q\n", fi.Name())
			switch {
			case fi.Mode()&os.ModeDir == os.ModeDir:
				//fmt.Printf("descend: dir: path=%q, fi.Name()=%q\n", path, fi.Name())
				stats.scannedDirs++
				if *dd != "" {
					b := ddre.MatchString(fi.Name())
					if b {
						//fmt.Printf("descend: skip dir=%q\n", fi.Name())
						continue
					}
				}
				for _, name := range ignoreList {
					if fi.Name() == name {
						fmt.Printf("descend: skip dir=%q\n", fi.Name())
						continue outer
					}
				}
				p := fullName(path, fi)
				//fmt.Printf("descend: dir=%q\n", p)
				d, err := os.Open(p)
				if err != nil {
					continue
				}
				fis, err := d.Readdir(-1)
				if err != nil || fis == nil {
					fmt.Printf("descend: can't read %q\n", fullName(path, fi))
					continue
				}
				d.Close()
				h, size := des(p, fis)
				hash = h
				gh.Write64(hash)
				sizes += size
				//fmt.Printf("descend: dir: path=%q, fi.Name()=%q, sizes=%d\n", path, fi.Name(), sizes)
				stats.matchedDirs++
				if dfp != nil {
					//fmt.Printf("descend: dfp: path=%q, fi.Name()=%q, hash=0x%016x, size=%d\n", path, fi.Name(), hash, size)
					dfp(path, fi, hash, size)
				}
			case fi.Mode()&os.ModeType == 0:
				stats.scannedFiles++
				if fi.Size() > fthreshold && (*pat == "" || (*pat != "" && patre.MatchString(fi.Name()))) {
					if *fr {
						hash = readFullHash(path, fi)
					} else {
						hash = readPartialHash(path, fi)
					}
					gh.Write64(hash)
					sizes += fi.Size()
					stats.matchedFiles++
					//fmt.Printf("descend: file: path=%q, fi.Name()=%q, hash=0x%016x, size=%d, sizes=%d\n", path, fi.Name(), hash, size, sizes)
					if ffp != nil {
						ffp(path, fi, hash, size)
					}
				}
			default:
				continue
			}
		}
		hashes := gh.Sum64()
		//fmt.Printf("descend: return dir=%q, hashes=0x%016x, sizes=%d\n", path, hashes, sizes)
		return hashes, sizes
	}
	//fmt.Printf("descend: path=%q\n", path)
	return des(path, fis)
}

func scan(paths []string, ndirs int) {
	var hash uint64
	var size int64

	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil || fi == nil {
			fmt.Printf("fi=%#v, err=%v\n", fi, err)
			panic("bad")
		}
		prefix := ""
		idx := strings.LastIndex(path, "/")
		if idx != -1 {
			prefix = path[0:idx]
		}
		switch {
		case fi.Mode()&os.ModeDir == os.ModeDir:
			fis := []os.FileInfo{fi}
			if *dirf {
				//hash, size = addDir(dir, fi)
				hash, size = descend(prefix, fis, nil, addDir)
				//add(hash, size, &kfe{prefix, size, hash})
			} else {
				//addDirs(path, fis)
				hash, size = descend(prefix, fis, addFile, nil)
			}
			//fmt.Printf("scan: dir  hash=0x%016x, path=%q, fi.Name()=%q\n\n", hash, prefix, fi.Name())
		case fi.Mode()&os.ModeType == 0:
			if *fr {
				hash = readFullHash(prefix, fi)
			} else {
				hash = readPartialHash(prefix, fi)
			}
			fmt.Printf("%016x %q\n", hash, path)
			//fmt.Printf("scan: file hash=0x%016x, path=%q, fi.Name()=%q\n\n", hash, path, fi.Name())
		}
		if *dirf && *ps {
			fmt.Printf("# dir=%q, hash=0x%016x, files totaling %h\n", path, hash, hrff.Int64{size, "B"})
		}
	}
}

type KFESlice []*kfe

func (s KFESlice) Len() int           { return len(s) }
func (s KFESlice) Less(i, j int) bool { return s[i].size < s[j].size }
func (s KFESlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type Indices []uint64

func (idx Indices) Len() int { return len(idx) }

//func (idx Indices) Less(i, j int) bool { return hmap[idx[i]][0].size > hmap[idx[j]][0].size }
func (idx Indices) Less(i, j int) bool {
	if *rs {
		return hmap[idx[i]][0].size < hmap[idx[j]][0].size
	} else {
		return hmap[idx[i]][0].size > hmap[idx[j]][0].size
	}
}

func (idx Indices) Swap(i, j int) { idx[i], idx[j] = idx[j], idx[i] }

var indicies Indices

func asort() {
	i := 0
	//fmt.Printf("asort: len(hmap)=%d\n", len(hmap))
	indicies = make(Indices, len(hmap))
	//fmt.Printf("asort: len(indicies)=%d\n", len(indicies))
	for k := range hmap {
		indicies[i] = k
		i++
	}
	sort.Sort(indicies)
}

func match(kind string, ndirs int) {
	//fmt.Printf("check: len(hmap)=%d\n", len(hmap))
	for k, v := range hmap {
		//fmt.Printf("check:\t%q %d %d\n", v[0].path, len(v), ndirs)
		if k == *fp {
			fmt.Printf("\t%q %d\n", v[0].path, len(v))
			for _, v2 := range v {
				fmt.Printf("\t\t%q\n", v2.path)
			}
		}
	}
	if *ps {
		fmt.Printf("# %d files, %d dirs scanned\n", stats.scannedFiles, stats.scannedDirs)
	}
}

func check(kind string, ndirs int) {
	//fmt.Printf("check: len(hmap)=%d\n", len(hmap))
	for k, v := range hmap {
		//fmt.Printf("check:\t%q %d %d\n", v[0].path, len(v), ndirs)
		switch {
		case *r && len(v) < ndirs && !*pd:
			count++
			if *print {
				fmt.Printf("\t%q %d %d\n", v[0].path, len(v), ndirs)
			}
		case *r && len(v) > ndirs && *pd:
			count++
			if *print {
				fmt.Printf("\t%q %d %d\n", v[0].path, len(v), ndirs)
			}
		case !*r && len(v) > 1:
			if len(v) > 1 {
				if *print {
					fmt.Printf("%016x ", k)
				}
				for k2, v2 := range v {
					size := hrff.Int64{v2.size, "B"}
					if k2 == 0 && *print {
						if *h {
							fmt.Printf("%h\n", size)
						} else {
							fmt.Printf("%d\n", size)
						}
					}
					total += v2.size
					count++
					if *print {
						fmt.Printf("\t%q\n", v2.path)
					}
				}
			}
		}
	}
	if *ps {
		if *r {
			fmt.Printf("# %d %s missing\n", count, kind)
		} else {
			fmt.Printf("# %d %s duplicated, totaling %h\n", count, kind, hrff.Int64{total, "B"})
		}
		fmt.Printf("# %d files, %d dirs scanned\n", stats.scannedFiles, stats.scannedDirs)
	}
}

func check2(kind string, ndirs int) {
	//fmt.Printf("check2: len(indicies)=%d\n", len(indicies))
	for _, vi := range indicies {
		v := hmap[vi]
		switch {
		case *r && len(v) < ndirs && !*pd:
			count++
			if *print {
				fmt.Printf("\t%q %d %d\n", v[0].path, len(v), ndirs)
			}
		case *r && len(v) > ndirs && *pd:
			count++
			if *print {
				fmt.Printf("\t%q %d %d\n", v[0].path, len(v), ndirs)
			}
		case !*r && len(v) > 1:
			if len(v) > 1 {
				if *print {
					fmt.Printf("%016x ", vi)
				}
				for k2, v2 := range v {
					size := hrff.Int64{v2.size, "B"}
					if k2 == 0 && *print {
						if *h {
							fmt.Printf("%h\n", size)
						} else {
							fmt.Printf("%d\n", size)
						}
					}
					total += v2.size
					count++
					if *print {
						fmt.Printf("\t%q\n", v2.path)
					}
				}
			}
		}
	}
	if *ps {
		if *r {
			fmt.Printf("# %d %s missing\n", count, kind)
		} else {
			fmt.Printf("# %d %s duplicated, totaling %h\n", count, kind, hrff.Int64{total, "B"})
		}
		fmt.Printf("# %d files, %d dirs scanned\n", stats.scannedFiles, stats.scannedDirs)
	}
}

func main() {
	var kind string = "files"
	var ndirs, nfiles int
	var paths []string

	flag.Var(&_fthreshold, "ft", "file sizes <= threshhold will not be considered")
	flag.Var(&_dthreshold, "dt", "directory sizes <= threshhold will not be considered")
	//fmt.Printf("dedup\n")
	hf = aeshash.NewAES(0)
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
	fthreshold = int64(_fthreshold.V)
	dthreshold = int64(_dthreshold.V)
	//fmt.Printf("fthreshold=%d\n", fthreshold)
	//fmt.Printf("dthreshold=%d\n", dthreshold)
	if *dirf {
		kind = "dirs"
	}

	if len(flag.Args()) != 0 {
		for _, path := range flag.Args() {
			fi, err := os.Stat(path)
			if err != nil || fi == nil {
				fmt.Printf("fi=%#v, err=%v\n", fi, err)
				panic("bad")
			}
			if fi.Mode()&os.ModeDir == os.ModeDir {
				ndirs++
			} else {
				nfiles++
			}
			paths = append(paths, path)
		}
	}

	scan(paths, ndirs)
	if *fp != 0 {
		fmt.Printf("Match\n")
		match(kind, ndirs)
		return
	}
	if *s {
		asort()
		check2(kind, ndirs)

	} else {
		check(kind, ndirs)
	}
}

/*
1. still a bug when comparing two dirs, there are two differnt top level hashses
2. with -r what happens with duplicated files? The count will not be ndirs and can be higher. Could chnage compare
   but what about 2 files in 2 dirs with a drop and an add would seem correct.
*/

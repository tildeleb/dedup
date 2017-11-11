// Copyright © 2015-2017 Lawrence E. Bakst. All rights reserved.

// dedup scans files or directories and calculates fingerprint hashes for them based on their contents.
//
// Originaly written on a plane from SFO->EWR on 7-23-15 in about an hour.
// Based on an idea I had been mulling in my mind for years.
//
// Without the -d (directory) switch dedup recursively scans the supplied directories in depth first
// order and records the hash of each file in a map of slices keyed by the hash. After the scan is
// complete, the resulting map is iterated, and if any of the slices have a length of more than 1,
// then the files on that slice are all duplicates of each other.
//
// If -d swicth is supplied the hashes of files in each directory are themselves recursively hashed and
// the resulting hashes of each directory (but not the files) are recorded in the map. Again, if the length
// of any slice is more than 1 then the entire directory is duplicated.
//
// The program works with more than two directories, but sometimes, not as well.
//
// The -p switch prints out the the requested information.
// The -ps switch just prints a summary of how many files or directories were duplicated
// and now much space they take up. Without a switch to print, no output is generated.
//
// If the -r switch is supplied, reverses the sense of the program and files or directories that
// ARE NOT duplicated are printed. When the map is scanned, any slices with a length different than
// the number of supplied directores are printed as these represent missing files. This allows
// directories to be easily compared and more than two can easily be compared. Even cooler is that
// the program works even if files or directories have been renamed.
//
// Exmaples
// % dedup -p ~/Desktop
// % dedup -d -p dir1 dir2
// % dedup -d -r -p dir1 dir2
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
	root int
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

var F = flag.Bool("F", false, "print fingerprint")
var S = flag.Bool("S", false, "print size")
var H = flag.Bool("H", false, "print human readable size")
var L = flag.Bool("L", false, "print length of hash chain")
var N = flag.Bool("N", false, "print number of roots")
var l = flag.Int("l", 1, "print fingerprints with more than l entries on chain")

var dirf = flag.Bool("d", false, "hash directories")
var r = flag.Bool("r", false, "reverse sense; display non-duplicated files")

var s = flag.Bool("s", false, "sort duplicate files by size from largest to smallest")
var rs = flag.Bool("rs", false, "reverse sense of sort, smallest to largest")
var fr = flag.Bool("fr", false, "full read; read the entire file")
var pat = flag.String("pat", "", "regexp pattern to match filenames")
var dd = flag.String("dd", "", "do not descend past dirs named this")
var print = flag.Bool("p", false, "print duplicated dirs or files")
var ps = flag.Bool("ps", false, "print summary")
var pd = flag.Bool("pd", false, "print duplicates with -r")
var fp = flag.Uint64("fp", 0, "fingerprint to search for.")

var blockSize = flag.Int64("b", 8192, "block size") // used when reading files

var _fthreshold = hrff.Int64{1, "B"}  // zero length files are excluded
var _dthreshold = hrff.Int64{-1, "B"} // zero length directories are incldued
var fthreshold int64
var dthreshold int64

var roots []string // list of directories passed on the command line
var hmap = make(map[uint64][]kfe, 100)
var smap = make(map[int64][]kfe, 100)
var hf hash.Hash64 = aeshash.NewAES(0)
var zeroHash = zhash() // the null hash (no bytes passed)
var ignoreList = []string{".Spotlight-V100", ".fseventsd"}
var total int64
var count int64

func fullName(path string, fi os.FileInfo) string {
	p := ""
	if path == "" {
		p = fi.Name()
	} else {
		p = path + "/" + fi.Name()
	}
	return p
}

func zhash() uint64 {
	hf.Reset()
	return hf.Sum64()
}

func readFullHash(path string, fi os.FileInfo) (r uint64) {
	p := fullName(path, fi)
	//fmt.Printf("readFullHash: path=%q fi.Name=%q, p=%q\n", path, fi.Name(), p)
	if fi.Size() == 0 {
		return zeroHash
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
		return zeroHash
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

func addFile(root int, path string, fi os.FileInfo, hash uint64, size int64) {
	p := fullName(path, fi)
	//fmt.Printf("addFile: path=%q, fi.Name()=%q, hash=%016x, p=%q\n", path, fi.Name(), hash, p)
	//fmt.Printf("addFile: hash=%016x, p=%q\n", hash, p)
	k1 := kfe{root, p, fi.Size(), 0}

	skey := fi.Size()
	// 0 length files are currently silently ignored they are not identical, is this still true?
	add(hash, skey, &k1)
	// smap not used
	_, ok2 := smap[skey]
	if !ok2 {
		smap[skey] = []kfe{k1}
	} else {
		smap[skey] = append(smap[skey], k1)
	}
}

func addDir(root int, path string, fi os.FileInfo, hash uint64, size int64) {
	if size <= dthreshold {
		return // should dirs respect threshold or is it only for files?
	}
	p := fullName(path, fi)
	//fmt.Printf("addDir: path=%q, fi.Name()=%q, p=%q, size=%d, hash=0x%016x\n", path, fi.Name(), p, size, hash)
	k1 := kfe{root, p, size, hash}
	add(hash, size, &k1)
}

func descend(root int, path string, fis []os.FileInfo,
	ffp func(root int, path string, fis os.FileInfo, hash uint64, size int64),
	dfp func(root int, path string, fis os.FileInfo, hash uint64, size int64)) (uint64, int64) {

	var des func(root int, path string, fis []os.FileInfo) (uint64, int64)
	des = func(root int, path string, fis []os.FileInfo) (uint64, int64) {
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
				h, size := des(root, p, fis)
				hash = h
				gh.Write64(hash)
				sizes += size
				//fmt.Printf("descend: dir: path=%q, fi.Name()=%q, sizes=%d\n", path, fi.Name(), sizes)
				stats.matchedDirs++
				if dfp != nil {
					//fmt.Printf("descend: dfp: path=%q, fi.Name()=%q, hash=0x%016x, size=%d\n", path, fi.Name(), hash, size)
					dfp(root, path, fi, hash, size)
				}
			case fi.Mode()&os.ModeType == 0:
				stats.scannedFiles++
				if fi.Size() >= fthreshold && (*pat == "" || (*pat != "" && patre.MatchString(fi.Name()))) {
					if *fr {
						hash = readFullHash(path, fi)
					} else {
						hash = readPartialHash(path, fi)
					}
					gh.Write64(hash)
					sizes += fi.Size()
					stats.matchedFiles++
					//fmt.Printf("descend: file: path=%q, fi.Name()=%q, hash=%016x, size=%d, sizes=%d\n", path, fi.Name(), hash, size, sizes)
					if ffp != nil {
						ffp(root, path, fi, hash, size)
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
	return des(root, path, fis)
}

func scan(paths []string, ndirs int) {
	var hash uint64
	var size int64

	for k, path := range paths {
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
				hash, size = descend(k, prefix, fis, nil, addDir)
				//add(hash, size, &kfe{prefix, size, hash})
			} else {
				//addDirs(path, fis)
				hash, size = descend(k, prefix, fis, addFile, nil)
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

var indicies Indices

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
	//fmt.Printf("check: kind=%q, ndirs=%d, len(hmap)=%d\n", kind, ndirs, len(hmap))
	for k, v := range hmap {
		//fmt.Printf("check:\t%q %d %d, %#x %#x\n", v[0].path, len(v), ndirs, k, *fp)
		if k == *fp {
			fmt.Printf("%q %d\n", v[0].path, len(v))
			for _, v2 := range v {
				fmt.Printf("\t%q\n", v2.path)
			}
		}
	}
	if *ps {
		fmt.Printf("# %d files, %d dirs scanned\n", stats.scannedFiles, stats.scannedDirs)
	}
}

func printLine(hash uint64, length, ndirs int, siz int64, path string) {
	if *F {
		fmt.Printf("%016x ", hash)
	}
	if *S {
		fmt.Printf("%d ", siz)
	}
	if *H {
		size := hrff.Int64{siz, "B"}
		s := fmt.Sprintf("%h", size)
		fmt.Printf("%s ", s)
	}
	if *N {
		fmt.Printf("%d ", ndirs)
	}
	if *L {
		fmt.Printf("%d ", length)
	}
	fmt.Printf("%q\n", path)
}

func printEntry(k uint64, v []kfe, ndirs int) {
	rootmask := uint64(0)
	rootcnts := make([]int, len(roots))
	mask := (uint64(1) << uint64(ndirs)) - 1
	for _, v2 := range v {
		rootmask |= 1 << uint64(v2.root)
		rootcnts[v2.root]++
	}
	rone := true
	for _, r := range rootcnts {
		if r != 1 {
			rone = false
		}
	}
	//fmt.Printf("checkFiles: ndirs=%d, len(v)=%d, rootmask=%b, mask=%b, rootcnts=%v\n", ndirs, len(v), rootmask, mask, rootcnts)
	//fmt.Printf("missing rootmask=%b, mask=%b, rootcnts=%v\n", rootmask, mask, rootcnts)
	switch {
	case *r && (len(v) < ndirs || (mask != rootmask) || !rone) && !*pd:
		for _, v2 := range v {
			total += v2.size
			count++
			printLine(k, len(v), ndirs, v2.size, v2.path)
		}
		fmt.Printf("\n")
	case *r && len(v) > ndirs && *pd:
		for _, v2 := range v {
			total += v2.size
			count++
			printLine(k, len(v), ndirs, v2.size, v2.path)
		}
	case !*r:
		if len(v) > 1 && *print {
			for _, v2 := range v {
				total += v2.size
				count++
				printLine(k, len(v), ndirs, v2.size, v2.path)
			}
			fmt.Printf("\n")
		}
	}
}

func check(kind string, ndirs int) {
	//fmt.Printf("check: kind=%q, ndirs=%d, len(hmap)=%d\n", kind, ndirs, len(hmap))
	for k, v := range hmap {
		//fmt.Printf("check:\t%q %d %d\n", v[0].path, len(v), ndirs)
		printEntry(k, v, ndirs)
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

func checks(kind string, ndirs int) {
	//fmt.Printf("check2: len(indicies)=%d\n", len(indicies))
	for _, vi := range indicies {
		v := hmap[vi]
		printEntry(vi, v, ndirs)
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

	flag.Var(&_fthreshold, "ft", "file sizes <= threshhold will not be considered")
	flag.Var(&_dthreshold, "dt", "directory sizes <= threshhold will not be considered")
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
	if *dirf {
		kind = "dirs"
	}

	if len(flag.Args()) != 0 {
		for _, root := range flag.Args() {
			fi, err := os.Stat(root)
			if err != nil || fi == nil {
				fmt.Printf("directory=%#v, err=%v, skipped\n", fi, err)
				continue
			}
			if fi.Mode()&os.ModeDir == os.ModeDir {
				ndirs++
			} else {
				nfiles++
			}
			roots = append(roots, root)
		}
	}

	scan(roots, ndirs)
	if *fp != 0 {
		match(kind, ndirs)
		return
	}
	if *s {
		asort()
		checks(kind, ndirs)

	} else {
		check(kind, ndirs)
	}
}

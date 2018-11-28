// Copyright © 2015-2017 Lawrence E. Bakst. All rights reserved.

// dedup scans files or directories and calculates fingerprint hashes for them based on their contents.
//
// Originally written on a plane from SFO->EWR on 7-23-15 in about an hour.
// Based on an idea I had been mulling in my mind for years.
//
// Without the -d (directory) switch dedup recursively scans the supplied directories in depth first
// order and records the hash of each file in a map of slices keyed by the hash. After the scan is
// complete, the resulting map is iterated, and if any of the slices have a length of more than 1,
// then all the files on that slice are all duplicates of each other.
//
// If -d switch is supplied the hashes of files in each directory are themselves recursively hashed and
// the resulting fingerprints for each directory (but not the files) are recorded in the map. Again, if the length
// of any slice is more than 1, then the entire directory is duplicated.
// The -d switch works with more than two directories, but sometimes not as well.
//
// If the -r switch is supplied, reverses the sense of the program and files or directories that
// ARE NOT duplicated are printed. When the map is scanned, any slices with a length different than
// the number of supplied directories are printed as these represent missing files. This allows
// directories to be easily compared and more than two can easily be compared. Even cooler is that
// the program works even if files or directories have been renamed.
//
// Without a switch to print, no output is generated.
// The -p prints out the pathnames of duplicated or missing files o directories.
// The -ps prints a summary of the number of files or dir that were duplicated and now much space they take up.
// The F, S, H, L, and N switches print the fingerprint, size, human readable size,
// hash chain length, and number of roots respectively.
//
// Examples
// % dedup -p ~/Desktop
// % dedup -d -p dir1 dir2
// % dedup -d -r -p dir1 dir2
//
// The hash used is the asehash from the Go runtime. It's fast and passes smhahser.
// The map of slices is not the most memory efficient representation and at some
// point it probably makes sense to switch to a cuckoo hash table.
//
package main

import (
	"flag"
	"fmt"
	"hash"
	"github.com/tildeleb/aeshash"
	"github.com/tildeleb/hrff"
	"github.com/tildeleb/siginfo"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
)

// One kfe is generated for each file or directory that is put into the hash map.
// The hash map entries point here to one of these.
// Space hasn't been an issue on my 0.5TB, 16 GB MacBook Pro.
type kfe struct {
	root  int
	level int
	path  string
	size  int64
	hash  uint64
}

type stat struct {
	scannedFiles int64
	scannedDirs  int64
	matchedFiles int64
	matchedDirs  int64
}

var F = flag.Bool("F", false, "print fingerprint")
var S = flag.Bool("S", false, "print size")
var H = flag.Bool("H", false, "print human readable size")
var L = flag.Bool("L", false, "print length of hash chain")
var N = flag.Bool("N", false, "print number of roots")
var l = flag.Int("l", 1, "print fingerprints with more than l entries on chain")

var dirf = flag.Bool("d", false, "hash directories")
var r = flag.Bool("r", false, "reverse sense; display non-duplicated files")

var v = flag.Bool("v", false, "verbose flag, print a line for each file or directory added")
var s = flag.Bool("s", false, "sort duplicate files by size from largest to smallest")
var rs = flag.Bool("rs", false, "reverse sense of sort, smallest to largest")
var fr = flag.Bool("fr", false, "full read; read the entire file")
var pat = flag.String("pat", "", "regexp pattern to match filenames")
var dd = flag.String("dd", "", "do not descend past dirs named this")
var print = flag.Bool("p", false, "print duplicated dirs or files")
var p0 = flag.Bool("p0", false, "print only level 0 files or dirs")
var p1 = flag.Bool("p1", false, "print only level 0 or 1 files or dirs")
var prune = flag.Int("prune", 999, "prune print to files of level or less")
var ps = flag.Bool("ps", false, "print summary")
var pd = flag.Bool("pd", false, "print duplicates with -r")
var fp = flag.Uint64("fp", 0, "fingerprint to search for.")
var blockSize = flag.Int64("b", 8192, "block size") // used when reading files
var _fthreshold = hrff.Int64{1, "B"}                // zero length files are excluded
var _dthreshold = hrff.Int64{-1, "B"}               // zero length directories are incldued
var fthreshold int64
var dthreshold int64

var hmap = make(map[uint64][]kfe, 100)
var smap = make(map[int64][]kfe, 100)
var hf hash.Hash64 = aeshash.NewAES(0)
var zeroHash = zhash() // the null hash (no bytes passed)
var ignoreList = []string{".DS_Store", ".Spotlight-V100", ".fseventsd", ".git"}
var ddre *regexp.Regexp
var patre *regexp.Regexp
var stats stat
var total int64
var count int64
var printOnePath bool
var phase = "scan"

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

// readFullHash reads an entire file and calculates the hash.
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

// readPartialHash reads a pieces of a file and calculates the hash.
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
		fmt.Printf("readPartialHash: %v (skipped)\n", err)
		return 0
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

// add a kfe to the hash map. check for inline
func add(hash uint64, size int64, k *kfe) {
	//fmt.Printf("add: kfe=%v\n", k)
	_, ok := hmap[hash]
	if !ok {
		hmap[hash] = []kfe{*k}
	} else {
		hmap[hash] = append(hmap[hash], *k)
	}
}

// addDir a file entry to the hash map.
func addFile(root, level int, path string, fi os.FileInfo, hash uint64, size int64) {
	p := fullName(path, fi)
	//fmt.Printf("addFile: path=%q, fi.Name()=%q, hash=%016x, p=%q\n", path, fi.Name(), hash, p)
	if *v {
		fmt.Printf("addFile: hash=%016x, p=%q\n", hash, p)
	}
	k1 := kfe{root, level, p, fi.Size(), 0}

	skey := fi.Size()
	add(hash, skey, &k1)
	// smap not used
	_, ok2 := smap[skey]
	if !ok2 {
		smap[skey] = []kfe{k1}
	} else {
		smap[skey] = append(smap[skey], k1)
	}
}

// addDir a directory entry to the hash map.
func addDir(root, level int, path string, fi os.FileInfo, hash uint64, size int64) {
	if size <= dthreshold {
		return // should dirs respect threshold or is it only for files?
	}
	p := fullName(path, fi)
	//fmt.Printf("addDir: path=%q, fi.Name()=%q, p=%q, size=%d, level=%d, hash=0x%016x\n", path, fi.Name(), p, size, level, hash)
	if *v {
		fmt.Printf("addDir : hash=%016x, p=%q\n", hash, p)
	}
	k1 := kfe{root, level, p, size, hash}
	add(hash, size, &k1)
}

// descent recursively descends the directory hierarchy.
func descend(root int, path string, fis []os.FileInfo,
	ffp func(root, level int, path string, fis os.FileInfo, hash uint64, size int64),
	dfp func(root, level int, path string, fis os.FileInfo, hash uint64, size int64)) (uint64, int64) {

	var level int = -1
	var des func(root int, path string, fis []os.FileInfo) (uint64, int64)
	des = func(root int, path string, fis []os.FileInfo) (uint64, int64) {
		var hash uint64
		var size, sizes int64
		var gh = aeshash.NewAES(0)

		level++
	outer:
		for _, fi := range fis {
			//fmt.Printf("descend: enter fi.Name=%q\n", fi.Name())
			if printOnePath {
				fmt.Printf("%d/%d \"%s/%s\"\n", stats.scannedFiles, stats.scannedDirs, path, fi.Name())
				printOnePath = false
			}
			switch {
			case fi.Mode()&os.ModeDir == os.ModeDir:
				//fmt.Printf("descend: dir: path=%q, fi.Name()=%q\n", path, fi.Name())
				stats.scannedDirs++
				for _, name := range ignoreList {
					if fi.Name() == name {
						//fmt.Printf("descend: skip dir=%q\n", fi.Name())
						continue outer
					}
				}
				if *dd != "" {
					b := ddre.MatchString(fi.Name())
					if b {
						//fmt.Printf("descend: skip dir=%q\n", fi.Name())
						continue
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
					dfp(root, level, path, fi, hash, size)
				}
			case fi.Mode()&os.ModeType == 0:
				stats.scannedFiles++
				for _, name := range ignoreList {
					if fi.Name() == name {
						//fmt.Printf("descend: skip file=%q\n", fi.Name())
						continue outer
					}
				}
				if fi.Size() >= fthreshold && (*pat == "" || (*pat != "" && patre.MatchString(fi.Name()))) {
					if *fr {
						hash = readFullHash(path, fi)
					} else {
						hash = readPartialHash(path, fi)
					}
					if hash == 0 {
						continue
					}
					gh.Write64(hash)
					sizes += fi.Size()
					stats.matchedFiles++
					//fmt.Printf("descend: file: path=%q, fi.Name()=%q, hash=%016x, size=%d, sizes=%d\n", path, fi.Name(), hash, size, sizes)
					if ffp != nil {
						ffp(root, level, path, fi, hash, size)
					}
				}
			default:
				continue
			}
		}
		hashes := gh.Sum64()
		//fmt.Printf("descend: return dir=%q, hashes=0x%016x, sizes=%d\n", path, hashes, sizes)
		level--
		return hashes, sizes
	}
	//fmt.Printf("descend: path=%q\n", path)
	return des(root, path, fis)
}

// scan the roots (dirs) and files passed on the command line and records their hashes in a map.
func scan(roots []string, files []string) {
	var hash uint64
	var size int64
	var s = [](*[]string){&files, &roots}

	for _, fds := range s {
		for k, path := range *fds {
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

func printLine(hash uint64, length, ndirs, level int, siz int64, path string) {
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
	fmt.Printf("%q %d\n", path, level)
}

// calcRootMembership given a slice of kfe, calculates if there is a kfe per root.
// It does this two different ways, first, a bit mask per root
// and the second, a count per root. It return if the file doesn't exist on all roots using the mask
// and if the root counts aren't all one.
// Need to decide if I can do better than this.
// In general, with fingerprints, there are some difficult edge cases.
// Todo: check for inline
func calcRootMembership(kfes []kfe, ndirs int) (bool, bool) {
	var rootmask uint64

	rootcnts := make([]int, ndirs)
	mask := (uint64(1) << uint64(ndirs)) - 1
	for _, kfe := range kfes {
		rootmask |= 1 << uint64(kfe.root)
		rootcnts[kfe.root]++
	}
	rone := true
	for _, r := range rootcnts {
		if r != 1 {
			rone = false
		}
	}
	//fmt.Printf("calcRootsMask: ndirs=%d, rootmask=%b, mask=%b, rootcnts=%v, rone=%v\n", ndirs, rootmask, mask, rootcnts, rone)
	return mask != rootmask, rone
}

// printEntry decides which entries to print
func printEntry(k uint64, v []kfe, ndirs int) {
	var pl = func() {
		for _, v2 := range v {
			if v2.level > *prune {
				continue
			}
			total += v2.size
			count++
			if *print {
				printLine(k, len(v), ndirs, v2.level, v2.size, v2.path)
			}
		}
		/*
			if *print {
				fmt.Printf("\n")
			}
		*/
	}
	// easy case, chain length more than 1, files are duplicated
	if !*r {
		if len(v) > 1 {
			pl()
			return
		}
	}
	// Below find cases for non-duplicated files
	neq, rone := calcRootMembership(v, ndirs)
	//fmt.Printf("printEntry: len(v)=%d, ndirs=%d, neq=%v, rone=%v, *pd=%v\n", len(v), ndirs, neq, rone, *pd)
	switch {
	case *r && len(v) <= ndirs && (neq || !rone) && !*pd: // one root doesn't have a dir or file
		pl()
	case *r && len(v) > ndirs && *pd: // probably at least one root has more than 1 copy of the file/dir
		pl()
	}
}

// check the kfes in random order and print them, print a summary if requested.
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

// check the kfes in sorted order and print them, print a summary if requested.
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

func f() {
	fmt.Printf("%s: ", phase)
	printOnePath = true
}

func main() {
	var roots []string // list of directories passed on the command line
	var files []string // list of files passed on the command line
	var kind string = "files"
	var ndirs, nfiles int

	flag.Var(&_fthreshold, "ft", "file sizes <= threshold will not be considered")
	flag.Var(&_dthreshold, "dt", "directory sizes <= threshold will not be considered")
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
	switch {
	case *p0:
		*prune = 0
	case *p1:
		*prune = 1
	}
	if len(flag.Args()) != 0 {
		for _, arg := range flag.Args() {
			fi, err := os.Stat(arg)
			if err != nil || fi == nil {
				fmt.Printf("directory=%#v, err=%v, skipped\n", fi, err)
				continue
			}
			if fi.Mode()&os.ModeDir == os.ModeDir {
				roots = append(roots, arg)
				ndirs++
			} else {
				files = append(files, arg)
				nfiles++
			}
		}
	}

	siginfo.SetHandler(f)
	scan(roots, files)

	if *fp != 0 {
		match(kind, ndirs)
		return
	}
	if *s {
		phase = "sort"
		asort()
		phase = "check"
		checks(kind, ndirs)

	} else {
		phase = "check"
		check(kind, ndirs)
	}
}

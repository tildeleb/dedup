dedup
=====
Copyright © 2014, 2015, 2016, 2017, 2018 Lawrence E. Bakst. All rights reserved.  

Summary
-------
Dedup identifies duplicated files or directories by constructing hash fingerprints of their contents. The default scan finds duplicated files but `-d` will scan for duplicated directories. Using `-r	` reversed the sense of the test and finds files or directories that are *not* duplicated.

Other options exist to cull the size of files examined, limit the depth of the recursive search, regular expression matching for files, sorting the results, and so on.

The directory search can be clipped by specifying a directory name with the `-dd` option. For example, on a Mac, the directory "Resources" is often a good choice.

Sometimes it makes sense to cull files or directories under a certain size and for this the `-dt` and `-ft` options can be used.

N.B. that because`-dt` will cull files it affects the fingerprints generated for directories with the `-d` option. For files only, filenames can be forced to match a pattern with `-pat`.

*The first version of this program was originally written on a plane from SFO->EWR on 7-23-15 in about an hour and was based on an idea I had been mulling in my mind for years. Dedup scans files or directories and calculates fingerprint hashes based on their contents and identifies duplicated or uniques files or directories.*

Usage
-----

	Usage of ./dedup:
  -C	print length of hash chain
  -F	print fingerprint
  -H	print human readable size
  -L	print level of directory or file, root is 0
  -N	print number of roots
  -S	print size
  -b int
    	block size (default 8192)
  -d	hash directories
  -dd string
    	do not descend past dirs named this
  -dpnl
    	don't print newline separators
  -dt value
    	directory sizes <= threshold will not be considered (default -1 B)
  -fp uint
    	fingerprint to search for.
  -fr
    	full read; read the entire file
  -ft value
    	file sizes <= threshold will not be considered (default 1 B)
  -l int
    	print fingerprints with more than l entries on chain (default 1)
  -p	print duplicated dirs or files
  -p0
    	print only level 0 files or dirs
  -p1
    	print only level 0 or 1 files or dirs
  -pat string
    	regexp pattern to match filenames
  -pd
    	print duplicates with -r
  -prune int
    	prune print to files of level or less (default 999)
  -ps
    	print summary
  -r	reverse sense; display non-duplicated files
  -rs
    	reverse sense of sort, smallest to largest
  -s	sort duplicate files by size from largest to smallest
  -v	verbose flag, print a line for each file or directory added

Examples
--------
	% # find duplicate files in d1
	% dedup -p d1
	% # find files not in both d1 and d2
	% dedup -d -r -p d1 d2
	% # assume that directories d1 d2 are versions of each other
	% # find differences between d1 and d2 but just print top level dirs that are different
	% dedup -d -r -p -p0 d1 d2

Output Format
-------------
	leb@hula-3:~/gotest/src/leb.io/dedup % dedup -p -F z z1
	0f6fc1f830df9250 "z/b1"
	0f6fc1f830df9250 "z/b2"
	
	858fdcc985d2e45f "z/big"
	858fdcc985d2e45f "z1/big"
	
	c3b9cb2d068916ae "z/b/f2"
	c3b9cb2d068916ae "z/c/f2"
	c3b9cb2d068916ae "z/README.md"
	c3b9cb2d068916ae "z/README2.md"
	c3b9cb2d068916ae "z1/b/f2"
	c3b9cb2d068916ae "z1/c/f2"
	c3b9cb2d068916ae "z1/README.md"
	c3b9cb2d068916ae "z1/README2.md"

With the `-F` flag for each set of duplicated files the hash is printed followed the pathname.

Implementation
--------------
dedup scans the supplied directories and/or files.

Without the -d (directory) switch dedup recursively scans the supplied directories in depth first order and records the hash of each file in a map of slices keyed by the hash. After the scan is complete, the resulting map is iterated, and if any of the slices have a length of more than one, then the files on that slice are all duplicates.

With the -d switch the supplied directories are recursively scanned and the fingerprint hashes of each directory are calculated from their files and sub-directories and recorded in the map. Again, if the length of any slice is more than one then the entire directory is duplicated.

If the -r switch is supplied, the sense of the test is reversed and files that are *not duplicated* are recorded and optionally printed. Useful when you want to know which files haven't been backed up. The reverse of duplicated files are missing files. They can be found with the -r switch.

Notes
-----
* For speed, only partial sections of large files are read and hashed.
* dedup doesn't consider path or file names in the logic of deciding if a file is duplicated or not. This has the benefit of being file name and path name (location) independent but has it's downsides too.
* *Originally written in about an hour on a plane from SFO->EWR, 7-23-15, and based on an idea I had been mulling for years.*



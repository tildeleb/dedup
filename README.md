dedup
=====
Copyright © 2014, 2015, 2016 Lawrence E. Bakst. All rights reserved.  
*currently has a few bugs which will be fixed soon*

Summary
-------
Dedup identifies duplicate or missing directories or files by constructing hash fingerprints of files and directories. The default scan is for duplicate files but `-d` will scan for duplicate directories.

Other options exist to cull the size of files examined, limit the depth of the recursive search, regular expression matching, sorting the results, and so on.

The directory search can be clipped by specifying a directory name with the `-dd` option. For example, on a Mac, the directory "Resources" is often a good choice.

Sometimes it makes sense to cull files or directories under a certain size and for this the `-dt` and `-ft` options can be used.

N.B. that because`-dt` will cull files it affects the fingerprints generated for directories with the `-d` option. For files only, filenames can be forced to match a pattern with `-pat`.

*The first version of this program was originally written on a plane from SFO->EWR on 7-23-15 in about an hour and was based on an idea I had been mulling in my mind for years. Dedup scans files or directories and calculates fingerprint hashes based on their contents and identifies duplicated or uniques files or directories.*


Implementation
--------------
dedup scans the supplied directories and/or files.

Without the -d (directory) switch dedup recursively scans the supplied directories in depth first order and records the hash of each file in a map of slices keyed by the hash. After the scan is complete, the resulting map is iterated, and if any of the slices have a length of more than one, then the files on that slice are all duplicates of each other.

With the -d switch the supplied directories are recursively scanned and the fingerprint hashes of each directory are calculated from their files and directories and recorded in the map. Again, if the length of any slice is more than one then the entire directory is duplicated.

If the -r switch is supplied, the sense of the test is reversed and files that are not duplicated are recorded and optionally printed. Useful when you want to know which files haven't been backed up. Missing files can be found with the -r switch.

	Usage of ./dedup:
	  -b int
	    	block size (default 8192)
	  -d	hash dirs
	  -dd string
	    	do not descend past dirs named this
	  -dt value
	    	directory sizes <= threshhold will not be considered
	  -fr
	    	full read; read the entire file
	  -ft value
	    	file sizes <= threshhold will not be considered
	  -h	human readable numbers
	  -p	print duplicated dirs or files
	  -pat string
	    	regexp pattern to match filenames
	  -pd
	    	print duplicates with -r
	  -ps
	    	print summary
	  -r	reverse sense; record non duplicated files
	  -rs
	    	reverse sense of sort, smallest to largest
	  -s	sort duplicate files by size from largest to smallest

Examples
--------
	dedup -p d1 d2
	dedup -p -d d1 d2
	dedup -r -p -d d1 d2

Output Format
-------------
	leb@hula:~/gotest/src/leb.io/dedup % ./dedup -p zzz             
	0554e24b2e799f49 3405
		"zzz/README1.md"
		"zzz/README2.md"
	leb@hula:~/gotest/src/leb.io/dedup % 

With the `-p` flag for each set of duplicated files the hash is printed and the file size followed by each pathname on separate lines.



Notes
-----
* For speed, only partial sections of large files are read and hashed.


* *Originally written in about an hour on a plane from SFO->EWR, 7-23-15, and based on an idea I had been mulling for years.*



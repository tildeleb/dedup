dedup
=====
Copyright © 2014,2015,2016 Lawrence E. Bakst. All rights reserved.  
*currently has a few bugs which will be fixed soon*

Summary
-------
Dedup identifies duplicate or missing directories or files by constructing hash fingerprints of files and directories. The default scan is for duplicate files but `-d` will scan for duplicate directories.

Missing files can be found with the -r switch.

The directory search can be clipped by specifying a directory name with the `-dd` option. On a Mac the directory "Resources" is often a good choice.

Sometimes it makes sense to cull files or directories under a certain size and for this the `-dt` and `-ft` options can be used.

N.B. that `-dt` will affect cull files and affect the fingerprints generated for directories with the `-d` option. For files only, filenames can be forced to match a pattern.

Implementation
--------------
dedup scans the supplied directories and/or files. Without the -d (directory) switch dedup recursively scans the supplied directories in depth first order and records the hash of each file in a map of slices keyed by the hash. After the scan is complete, the resulting map is iterated and if any of the slices have a length of more than 1, then the files on that slice are al duplicates.

If -d switch is supplied the hashes of files are themselves recursively hashed and the resulting hashes of each directory are recorded in the map. Again, if the length of any slice is more than 1 then the entire directory is duplicated.

If the -r switch is supplied, when the map is scanned, any slices with a length different than the number of supplied directores are printed as these represent missing files. This allows directories to be easily compare and more than two can easily be compared.


	Usage of dedup:
	  -b int
	    	block size (default 8192)
	  -d	hash dirs
	  -dd string
	    	do not descend past dirs named this
	  -dt value
	    	directory sizes <= threshhold will not be considered (default 0 )
	  -fr
	    	full read; read the entire file
	  -ft value
	    	file sizes <= threshhold will not be considered (default 0 )
	  -p	print duplicated dirs or files
	  -pat string
	    	regexp pattern to match filenames
	  -pd
	    	print duplicates with -r
	  -ps
	    	print summary
	  -r	reverse sense; record non duplicated files

Examples
--------
	dedup -p d1 d2
	dedup -p -d d1 d2
	dedup -r -p -d d1 d2

Output Format
-------------
Designed for easy parsing by standard tools, but still a work in progress.

Notes
-----
* For speed large files only have the beginning and end hashed.

* First cut, not much error checking, needs much work.

* *Originally written in about an hour on a plane from SFO->EWR, 7-23-15, and based on an idea I had been mulling for years.*



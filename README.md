dedup
=====

Summary
-------
Dedup identifies duplicate directories or files by constructing hash fingerprints. The default scan is for duplicate files but `-d` will scan for duplicate directories.

Normally summary information is printed. The specific files or directories can be printed with the `-p` option.

The directory search can be clipped by specifying a directory name with the `-dd` option. On a Mac the directory "Resources" is often a good choice.

Sometimes it makes sense to cull files or directories under a certain size and for this the `-dt` and `-ft` options can be used.

N.B. that `-dt` will affect cull files and affect the fingerprints generated for directories with the `-d` option. For files only, filenames can be forced to match a pattern.

	Usage of ./dedup [options] dirs
	  -b int
	    	block size (default 8192)
	  -d	hash dirs
	  -dd string
	    	do not descend past dirs named this
	  -dt value
	    	directory sizes <= threshhold will not be considered (default 0 )
	  -ft value
	    	file sizes <= threshhold will not be considered (default 0 )
	  -p	print duplicated dirs or files
	  -pat string
	    	regexp pattern to match filenames

Examples
--------

Output Format
-------------

Implementation
--------------
* For speed large files only have the beginning and end hashed.

* First cut, not much error checking, needs much work.

* *originaly written in about an hour on plane from SFO->EWR 7-23-15*



// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const (
	defaultCNIBinSrcDir = "/usr/src/cni/bin"
	defaultCNIBinDir    = "/host/opt/cni/bin"
)

func usage() {
	fmt.Fprintf(os.Stderr,
		"This is an entrypoint for CNI plugins to overlay their\n"+
			"binaries into a location in a filesystem. All files from\n"+
			"the source directory will be copied to the destination directory.\n\n"+
			"./entrypoint\n"+
			"\t-h --help\n"+
			"\t--cni-bin-src-dir=%s\n"+
			"\t--cni-bin-dir=%s\n",
		defaultCNIBinSrcDir, defaultCNIBinDir)
}

func run() int {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	cniBinSrcDir := fs.String("cni-bin-src-dir", defaultCNIBinSrcDir, "Source directory containing CNI plugin binaries")
	cniBinDir := fs.String("cni-bin-dir", defaultCNIBinDir, "CNI binary destination directory")
	fs.Usage = usage

	err := fs.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to parse flags: %v\n", err)
		return 1
	}

	cniBinSrcDirClean := filepath.Clean(*cniBinSrcDir)
	if !filepath.IsAbs(cniBinSrcDirClean) {
		fmt.Fprintf(os.Stderr, "cni-bin-src-dir must be an absolute path, got: %s\n", *cniBinSrcDir)
		return 1
	}

	cniBinDirClean := filepath.Clean(*cniBinDir)
	if !filepath.IsAbs(cniBinDirClean) {
		fmt.Fprintf(os.Stderr, "cni-bin-dir must be an absolute path, got: %s\n", *cniBinDir)
		return 1
	}

	srcInfo, err := os.Stat(cniBinSrcDirClean)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cni-bin-src-dir %q does not exist: %v\n", cniBinSrcDirClean, err)
		return 1
	}
	if !srcInfo.IsDir() {
		fmt.Fprintf(os.Stderr, "cni-bin-src-dir %q is not a directory\n", cniBinSrcDirClean)
		return 1
	}

	dstInfo, err := os.Stat(cniBinDirClean)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cni-bin-dir %q does not exist: %v\n", cniBinDirClean, err)
		return 1
	}
	if !dstInfo.IsDir() {
		fmt.Fprintf(os.Stderr, "cni-bin-dir %q is not a directory\n", cniBinDirClean)
		return 1
	}

	if err := copyDir(cniBinSrcDirClean, cniBinDirClean); err != nil {
		fmt.Fprintf(os.Stderr, "failed to copy CNI binaries: %v\n", err)
		return 1
	}

	fmt.Println("CNI plugin binaries installed, waiting for termination signal.")
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(ch)
	<-ch
	return 0
}

// copyDir copies all regular files from src directory into dst directory.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read source directory %q: %w", src, err)
	}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		srcPath := filepath.Join(src, entry.Name())
		tempPattern := fmt.Sprintf("%s.temp", entry.Name())
		if err := copyFileAtomic(srcPath, dst, tempPattern, entry.Name()); err != nil {
			return fmt.Errorf("copy %q: %w", entry.Name(), err)
		}
	}
	return nil
}

// copyFileAtomic does file copy atomically
func copyFileAtomic(srcFilePath, destDir, tempFileName, destFileName string) error {
	tempFilePath := filepath.Join(destDir, tempFileName)
	// check temp filepath and remove old file if exists
	if _, err := os.Stat(tempFilePath); err == nil {
		err = os.Remove(tempFilePath)
		if err != nil {
			return fmt.Errorf("cannot remove old temp file %q: %v", tempFilePath, err)
		}
	}

	// create temp file
	f, err := os.CreateTemp(destDir, tempFileName)
	if err != nil {
		return fmt.Errorf("cannot create temp file %q in %q: %v", tempFileName, destDir, err)
	}
	defer f.Close()

	srcFile, err := os.Open(srcFilePath)
	if err != nil {
		return fmt.Errorf("cannot open file %q: %v", srcFilePath, err)
	}
	defer srcFile.Close()

	// Copy file to tempfile
	_, err = io.Copy(f, srcFile)
	if err != nil {
		f.Close()
		os.Remove(tempFilePath)
		return fmt.Errorf("cannot write data to temp file %q: %v", tempFilePath, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("cannot flush temp file %q: %v", tempFilePath, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("cannot close temp file %q: %v", tempFilePath, err)
	}

	// change file mode if different
	destFilePath := filepath.Join(destDir, destFileName)
	_, err = os.Stat(destFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	srcFileStat, err := os.Stat(srcFilePath)
	if err != nil {
		return err
	}

	if err := os.Chmod(f.Name(), srcFileStat.Mode()); err != nil {
		return fmt.Errorf("cannot set stat on temp file %q: %v", f.Name(), err)
	}

	// replace file with tempfile
	if err := os.Rename(f.Name(), destFilePath); err != nil {
		return fmt.Errorf("cannot replace %q with temp file %q: %v", destFilePath, tempFilePath, err)
	}

	return nil
}

func main() {
	os.Exit(run())
}

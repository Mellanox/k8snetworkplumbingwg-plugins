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
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeTempFile creates a temporary file with the given content and permissions.
func writeTempFile(dir, name string, content []byte, perm os.FileMode) string {
	path := filepath.Join(dir, name)
	Expect(os.WriteFile(path, content, perm)).To(Succeed())
	return path
}

var _ = Describe("copyFile", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "entrypoint-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, tmpDir)
	})

	It("copies file content to destination", func() {
		content := []byte("binary-content")
		src := writeTempFile(tmpDir, "src", content, 0o644)
		dst := filepath.Join(tmpDir, "dst")

		Expect(copyFileAtomic(src, tmpDir, "src.temp", "dst")).To(Succeed())

		got, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(content))
	})

	It("preserves source file permissions on the destination", func() {
		src := writeTempFile(tmpDir, "src", []byte("data"), 0o755)
		dst := filepath.Join(tmpDir, "dst")

		Expect(copyFileAtomic(src, tmpDir, "src.temp", "dst")).To(Succeed())

		info, err := os.Stat(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o755)))
	})

	It("overwrites an existing destination file", func() {
		src := writeTempFile(tmpDir, "src", []byte("new-content"), 0o755)
		dst := writeTempFile(tmpDir, "dst", []byte("old-content"), 0o644)

		Expect(copyFileAtomic(src, tmpDir, "src.temp", "dst")).To(Succeed())

		got, err := os.ReadFile(dst)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal([]byte("new-content")))
	})

	It("returns an error when the source does not exist", func() {
		err := copyFileAtomic(filepath.Join(tmpDir, "nonexistent"), tmpDir, "src.temp", "dst")
		Expect(err).To(HaveOccurred())
	})

	It("returns an error when the destination directory does not exist", func() {
		src := writeTempFile(tmpDir, "src", []byte("data"), 0o755)
		err := copyFileAtomic(src, filepath.Join(tmpDir, "no-such-dir"), "src.temp", "dst")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("copyDir", func() {
	var (
		srcDir string
		dstDir string
	)

	BeforeEach(func() {
		var err error
		srcDir, err = os.MkdirTemp("", "copydir-src-*")
		Expect(err).NotTo(HaveOccurred())
		dstDir, err = os.MkdirTemp("", "copydir-dst-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			os.RemoveAll(srcDir)
			os.RemoveAll(dstDir)
		})
	})

	It("copies all regular files from src to dst", func() {
		writeTempFile(srcDir, "plugin-a", []byte("plugin-a-data"), 0o755)
		writeTempFile(srcDir, "plugin-b", []byte("plugin-b-data"), 0o755)

		Expect(copyDir(srcDir, dstDir)).To(Succeed())

		for _, name := range []string{"plugin-a", "plugin-b"} {
			got, err := os.ReadFile(filepath.Join(dstDir, name))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal([]byte(name + "-data")))
		}
	})

	It("skips non-regular files (directories)", func() {
		Expect(os.Mkdir(filepath.Join(srcDir, "subdir"), 0o755)).To(Succeed())
		writeTempFile(srcDir, "plugin", []byte("data"), 0o755)

		Expect(copyDir(srcDir, dstDir)).To(Succeed())

		_, err := os.Stat(filepath.Join(dstDir, "subdir"))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Expect(filepath.Join(dstDir, "plugin")).To(BeAnExistingFile())
	})

	It("succeeds when source directory is empty", func() {
		Expect(copyDir(srcDir, dstDir)).To(Succeed())
	})

	It("returns an error when source directory does not exist", func() {
		err := copyDir(filepath.Join(srcDir, "no-such-dir"), dstDir)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("run", func() {
	var (
		origArgs []string
		tmpDir   string
	)

	BeforeEach(func() {
		origArgs = os.Args
		var err error
		tmpDir, err = os.MkdirTemp("", "entrypoint-run-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			os.Args = origArgs
			os.RemoveAll(tmpDir)
		})
	})

	It("returns 1 when --cni-bin-src-dir is a relative path", func() {
		os.Args = []string{"entrypoint", "--cni-bin-src-dir=relative/path", "--cni-bin-dir=" + tmpDir}
		Expect(run()).To(Equal(1))
	})

	It("returns 1 when --cni-bin-dir is a relative path", func() {
		os.Args = []string{"entrypoint", "--cni-bin-src-dir=" + tmpDir, "--cni-bin-dir=relative/path"}
		Expect(run()).To(Equal(1))
	})

	It("returns 1 when --cni-bin-src-dir does not exist", func() {
		os.Args = []string{"entrypoint",
			"--cni-bin-src-dir=" + filepath.Join(tmpDir, "no-such-dir"),
			"--cni-bin-dir=" + tmpDir,
		}
		Expect(run()).To(Equal(1))
	})

	It("returns 1 when --cni-bin-dir does not exist", func() {
		os.Args = []string{"entrypoint",
			"--cni-bin-src-dir=" + tmpDir,
			"--cni-bin-dir=" + filepath.Join(tmpDir, "no-such-dir"),
		}
		Expect(run()).To(Equal(1))
	})

	It("returns 1 when --cni-bin-src-dir points to a file, not a directory", func() {
		notADir := writeTempFile(tmpDir, "notadir", []byte("x"), 0o644)
		os.Args = []string{"entrypoint",
			"--cni-bin-src-dir=" + notADir,
			"--cni-bin-dir=" + tmpDir,
		}
		Expect(run()).To(Equal(1))
	})

	It("returns 1 when --cni-bin-dir points to a file, not a directory", func() {
		notADir := writeTempFile(tmpDir, "notadir", []byte("x"), 0o644)
		os.Args = []string{"entrypoint",
			"--cni-bin-src-dir=" + tmpDir,
			"--cni-bin-dir=" + notADir,
		}
		Expect(run()).To(Equal(1))
	})

	It("copies all binaries and exits 0 on SIGTERM", func() {
		srcDir, err := os.MkdirTemp("", "entrypoint-src-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, srcDir)
		dstDir, err := os.MkdirTemp("", "entrypoint-dst-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(os.RemoveAll, dstDir)

		pluginContent := []byte("#!/bin/sh\necho plugin")
		writeTempFile(srcDir, "bridge", pluginContent, 0o755)
		writeTempFile(srcDir, "loopback", pluginContent, 0o755)

		// Build the entrypoint binary so we can run it as a child process.
		// This avoids sending SIGTERM to the test process itself.
		entrypointBin := filepath.Join(tmpDir, "entrypoint")
		buildOut, err := exec.Command("go", "build", "-o", entrypointBin, ".").CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "go build failed: %s", buildOut)

		proc := exec.Command(entrypointBin,
			"--cni-bin-src-dir="+srcDir,
			"--cni-bin-dir="+dstDir,
		)
		proc.Stdout = GinkgoWriter
		proc.Stderr = GinkgoWriter
		Expect(proc.Start()).To(Succeed())
		DeferCleanup(func() { _ = proc.Process.Kill() })

		// Poll until both files have been copied.
		for _, name := range []string{"bridge", "loopback"} {
			destFile := filepath.Join(dstDir, name)
			Eventually(destFile, 5*time.Second, 50*time.Millisecond).Should(BeAnExistingFile())

			got, err := os.ReadFile(destFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(pluginContent))
			info, err := os.Stat(destFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o755)))
		}

		Expect(proc.Process.Signal(syscall.SIGTERM)).To(Succeed())
		Expect(proc.Wait()).To(Succeed())
		Expect(proc.ProcessState.ExitCode()).To(Equal(0))
	})
})

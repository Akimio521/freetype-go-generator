package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"modernc.org/cc/v4"
	ccgo "modernc.org/ccgo/v4/lib"
)

const (
	freetypeHeaderDir = "freetype/include"
	aname             = "objs/.libs/libfreetype.a"
	result            = "ccgo.go"
	packageName       = "libfreetype"
	target            = runtime.GOOS + "/" + runtime.GOARCH
)

var (
	make string
	sed  string
	j    = fmt.Sprint(runtime.GOMAXPROCS(-1))
)

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runInDir(dir string, fn func() error) error {
	oldDir, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(dir); err != nil {
		return err
	}

	return fn()
}

func CopyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()
		dstFile, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()
		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func cleanupSubmodule(submodulePath string) error {
	if err := runCommand("git", "-C", submodulePath, "clean", "-fdx"); err != nil {
		return err
	}
	if err := runCommand("git", "-C", submodulePath, "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	return nil
}

func main() {
	if ccgo.IsExecEnv() {
		if err := ccgo.NewTask(runtime.GOOS, runtime.GOARCH, os.Args, os.Stdout, os.Stderr, nil).Main(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return
	}

	fmt.Println("buiding for", target)

	switch runtime.GOOS {
	case "linux":
		make = "make"
		sed = "sed"

	case "darwin":
		make = "make"
		sed = "gsed"

	case "freebsd", "openbsd":
		make = "gmake"
		sed = "gsed"

	case "windows":
		make = "make"
		sed = "sed"

	default:
		panic(fmt.Sprintf("unsupported target %s", target))
	}

	libRoot, err := filepath.Abs("freetype")
	if err != nil {
		panic(fmt.Sprintf("cannot get absolute path for freetype: %v", err))
	}

	makeRoot := libRoot

	// clean change for freetype submodule
	if err := cleanupSubmodule(libRoot); err != nil {
		panic(fmt.Sprintf("failed to cleanup submodule: %v", err))
	}

	CopyDir(filepath.Join("internal", "overlay", "all"), libRoot)

	if err := runInDir(makeRoot, func() error {
		if err := runCommand("go", "mod", "init", "example.com/libfreetype"); err != nil {
			return err
		}
		if err := runCommand("go", "get", "modernc.org/libc@latest", "modernc.org/libz@latest"); err != nil {
			return err
		}

		// Run autogen.sh to generate configure script
		if err := runCommand("sh", "./autogen.sh"); err != nil {
			return err
		}

		ccgoConfig := []string{os.Args[0]}
		cflags := []string{"-DFT_CONFIG_OPTION_NO_ASSEMBLER"}
		switch target {
		case "linux/amd64":
			if err := runCommand(sed, "-i", `s/FT_SSE2 1/FT_SSE2 0/g`, "src/smooth/ftgrays.c"); err != nil {
				return err
			}
		}
		m64Double := cc.LongDouble64Flag(runtime.GOOS, runtime.GOARCH)
		if m64Double != "" {
			ccgoConfig = append(ccgoConfig, m64Double)
		}

		freetypeConfigs := []string{"--disable-shared", "--with-brotli=no", "--with-bzip2=no", "--with-harfbuzz=no", "--with-png=no", "--with-zlib=yes"}
		cmdLine := "CFLAGS=" + strings.Join(cflags, " ") + " ./configure " + strings.Join(freetypeConfigs, " ")
		err := runCommand("sh", "-c", cmdLine)
		if err != nil {
			return fmt.Errorf("failed to run configure: %w", err)
		}

		ccgoConfig = append(ccgoConfig,
			"--package-name", packageName,
			"--prefix-enumerator=_",
			"--prefix-external=x_",
			"--prefix-field=F",
			"--prefix-macro=m_",
			"--prefix-static-internal=_",
			"--prefix-static-none=_",
			"--prefix-tagged-enum=_",
			"--prefix-tagged-struct=T",
			"--prefix-tagged-union=T",
			"--prefix-typename=T",
			"--prefix-undefined=_",
			"-ignore-unsupported-alignment",
			"-I", freetypeHeaderDir,
		)

		err = ccgo.NewTask(runtime.GOOS, runtime.GOARCH, append(ccgoConfig, "-exec", make, "-j", j, "library"), os.Stdout, os.Stderr, nil).Exec()
		if err != nil {
			return fmt.Errorf("failed to build freetype library: %w", err)
		}
		fmt.Println("freetype library built successfully, starting to generate ccgo bindings...")

		err = ccgo.NewTask(runtime.GOOS, runtime.GOARCH, append(ccgoConfig, "-o", result, aname, "-lz"), os.Stdout, os.Stderr, nil).Main()
		if err != nil {
			return fmt.Errorf("failed to generate ccgo bindings: %w", err)
		}
		fmt.Println("ccgo bindings generated successfully, starting to clean up...")

		err = runCommand(sed, "-i", `s/\<T__\([a-zA-Z0-9][a-zA-Z0-9_]\+\)/t__\1/g`, result)
		if err != nil {
			return fmt.Errorf("failed to replace T__ with t__: %w", err)
		}
		err = runCommand(sed, "-i", `s/\<x_\([a-zA-Z0-9][a-zA-Z0-9_]\+\)/X\1/g`, result)
		if err != nil {
			return fmt.Errorf("failed to replace x_ with X: %w", err)
		}
		err = runCommand(sed, "-i", `/^[[:space:]]*\/\/ Code generate/,+1d`, result)
		if err != nil {
			return fmt.Errorf("failed to remove code generation comment: %w", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}

	CopyFile(filepath.Join(libRoot, result), packageName+"/"+"ccgo"+"_"+runtime.GOOS+"_"+runtime.GOARCH+".go")

	if err := cleanupSubmodule(libRoot); err != nil {
		panic(fmt.Sprintf("failed to cleanup submodule: %v", err))
	}
	fmt.Printf("Successfully generated ccgo bindings for %s/%s and cleaned up submodule\n", runtime.GOOS, runtime.GOARCH) //nolint:forbidigo
}

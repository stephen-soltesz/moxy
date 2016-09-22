package main

import (
	"crypto/tls"
	"os/exec"
	// "crypto/x509"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"text/template"
)

var (
	url    = flag.String("url", "https://127.0.0.1:8081/", "The https url to get.")
	output = flag.String("o", "", "The output file name.")
)

//////////////////////////
//
// nextstage structure contains one of:
// * kexec
// * chain
// * command
//
//////////////////////////

type KexecSource struct {
	Vmlinuz   string // Fully qualified URI to vmlinuz image.
	Initramfs string // Fully qualified URI to initramfs image.
	Kargs     string // Additional kernel paramters.
	Command   string // Command for kexec. Interpreted as a Go template.
}

type ChainSource struct {
	Nextboot string // Source file.
}

type FallbackSource struct {
	Source string // Source file.
}

type Nextboot struct {
	Kexec     *KexecSource
	Chain     *ChainSource
	Fallback  *FallbackSource
	SessionId string
}

func (n *Nextboot) String() string {
	// Errors only occur for non-UTF8 characters in strings.
	// AddHostInformation checks that strings are valid utf8.
	b, _ := json.MarshalIndent(n, "", "    ")
	return string(b)
}

func tmp(name string) *os.File {
	t, err := ioutil.TempFile("", name)
	if err != nil {
		log.Fatal(err)
	}
	return t
}

func Load(input string) (*Nextboot, error) {
	b, err := ioutil.ReadFile(input)
	if err != nil {
		return nil, err
	}
	n := &Nextboot{}
	err = json.Unmarshal(b, n)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func Download(client *http.Client, url, output string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Dump response
	f, err := os.Create(output)
	if err != nil {
		return err
	}
	// TODO: retry or resume on error.
	log.Printf("Downloading: %s\n", output)
	l, err := io.Copy(f, resp.Body)
	if l != resp.ContentLength {
		return fmt.Errorf("Expected ContentLength(%d) actually read(%d)", resp.ContentLength, l)
	}
	log.Printf("Wrote: %d bytes\n", l)
	return nil
}

func loadKexec(client *http.Client, kexec *KexecSource) error {
	// Download the vmlinuz and initramfs images.
	vmlinuz := tmp("vmlinuz")
	err := Download(client, kexec.Vmlinuz, vmlinuz.Name())
	if err != nil {
		log.Fatal(err)
	}
	// defer os.Remove(vmlinuz.Name())

	initramfs := tmp("initramfs")
	err = Download(client, kexec.Initramfs, initramfs.Name())
	if err != nil {
		log.Fatal(err)
	}
	// defer os.Remove(initramfs.Name())

	// Save local temporary file names for evaluating command template.
	k := KexecSource{
		Vmlinuz:   vmlinuz.Name(),
		Initramfs: initramfs.Name(),
		Kargs:     kexec.Kargs,
	}
	cmd := parseCommand(k, kexec.Command)
	log.Printf("# %s\n", cmd)

	// TODO(soltesz): make this better.
	c := exec.Command("/bin/sh", "-c", cmd)

	// This should not return, but if it does, we want to log all output.
	output, err := c.CombinedOutput()
	log.Printf("Error: %s\n", err)
	log.Printf("Output: %s\n", output)
	return err
}

func loadFallback(client *http.Client, f *FallbackSource) error {
	fallback := tmp("fallback")
	err = Download(client, f.Fallback, fallback.Name())
	if err != nil {
		log.Fatal(err)
	}

	// cmd := fmt.Sprintf("tar xvf %s", fallback.Name())
	// c := exec.Command("/bin/sh", "-c", "fallback/fallback")
	return nil
}

func parseCommand(kexec KexecSource, cmd string) string {

	// Parse command as a template.
	tmpl, err := template.New("name").Parse(cmd)
	if err != nil {
		log.Fatal(err)
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, kexec)
	if err != nil {
		log.Fatal(err)
	}
	return string(b.Bytes())
}

func processURI(client *http.Client, uri string) error {
	// Get and parse the nextboot configuration.
	nextboot := tmp("nextboot")
	log.Printf("Downloading %s -> %s\n", uri, nextboot.Name())
	err := Download(client, uri, nextboot.Name())
	if err != nil {
		log.Fatal(err)
	}
	// defer os.Remove(nextboot.Name())

	// Load the configuration.
	n, err := Load(nextboot.Name())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%s\n", n.String())

	if n.Kexec != nil {
		err = loadKexec(client, n.Kexec)
	} else if n.Chain != nil {
		err = processURI(client, n.Chain.Nextboot)
	} else if n.Fallback != nil {
		err = loadFallback(client, n.Fallback)
	} else {
		err = nil
	}
	return err
}

func main() {
	flag.Parse()
	// if *output == "" {
	//      log.Fatal("Specify the output file name: -o <filename>")
	// }
	// fmt.Printf("%s -> %s\n", *url, *output)

	// certPool, err := x509.SystemCertPool()
	// if err != nil {
	//     log.Fatal(err)
	// }

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		// RootCAs:      certPool,
		MinVersion: tls.VersionTLS10,
	}
	tlsConfig.BuildNameToCertificate()
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	err := processURI(client, *url)
	if err != nil {
		log.Fatal(err)
	}
	// Download and parse the nextboot configuration.
	// nextboot := tmp("nextboot")
	// log.Printf("%s -> %s\n", *url, nextboot.Name())
	// err := Download(client, *url, nextboot.Name())
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer os.Remove(nextboot.Name())

	// Load the configuration.
	// n, err := Load(nextboot.Name())
	// if err != nil {
	//     log.Fatal(err)
	// }
	// log.Printf("%s\n", n.String())

	// if n.Kexec != nil {
	// 	err = loadKexec(client, n.Kexec)
	// } else if n.Chain != nil {
	// 	err = processURI(client, n.Chain.Nextboot)
	// } else if n.Fallback != nil {
	// 	err = loadFallback(n.Fallback)
	// } else {
	// 	err = nil
	// }
	// os.Exit(1)
	os.Exit(1)
}

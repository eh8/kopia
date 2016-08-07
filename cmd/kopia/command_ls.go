package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kopia/kopia/fs"
	"github.com/kopia/kopia/repo"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	lsCommand = app.Command("ls", "List a directory stored in repository object.")

	lsCommandLong = lsCommand.Flag("long", "Long output").Short('l').Bool()
	lsCommandPath = lsCommand.Arg("path", "Path").Required().String()
)

func runLSCommand(context *kingpin.ParseContext) error {
	vlt := mustOpenVault()
	mgr, err := vlt.OpenRepository()
	if err != nil {
		return err
	}

	oid, err := parseObjectID(*lsCommandPath, vlt)
	if err != nil {
		return err
	}

	var prefix string
	if !*lsCommandLong {
		prefix = *lsCommandPath
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
	}

	dir := fs.NewRepositoryDirectory(mgr, oid)
	entries, err := dir.Readdir()
	if err != nil {
		return err
	}

	listDirectory(prefix, entries, *lsCommandLong)

	return nil
}

func init() {
	lsCommand.Action(runLSCommand)
}

func listDirectory(prefix string, entries fs.Entries, longFormat bool) {
	maxNameLen := 20
	for _, e := range entries {
		m := e.Metadata()
		if l := len(m.Name); l > maxNameLen {
			maxNameLen = l
		}
	}

	maxNameLenString := strconv.Itoa(maxNameLen)

	for _, e := range entries {
		m := e.Metadata()
		var info string
		if longFormat {
			var oid string
			if m.ObjectID.Type() == repo.ObjectIDTypeBinary {
				oid = "<inline binary>"
			} else if m.ObjectID.Type() == repo.ObjectIDTypeText {
				oid = "<inline text>"
			} else {
				oid = string(m.ObjectID)
			}
			info = fmt.Sprintf("%v %9d %v %-"+maxNameLenString+"s %v", m.FileMode, m.FileSize, m.ModTime.Local().Format("02 Jan 06 15:04:05"), m.Name, oid)
		} else {
			var suffix string
			if m.FileMode.IsDir() {
				suffix = "/"
			}

			info = prefix + m.Name + suffix
		}
		fmt.Println(info)
	}
}
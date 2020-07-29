// +build linux

package util

import (
	"fmt"
	"github.com/devspace-cloud/devspace/pkg/devspace/build/builder/restart"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type containerRestarter struct {
}

func NewContainerRestarter() ContainerRestarter {
	return &containerRestarter{}
}

func (*containerRestarter) RestartContainer() error {
	pidFilePath := restart.ProcessIDFilePath

	// check if restart script is there
	_, err := os.Stat(restart.LegacyScriptPath)
	if err == nil {
		pidFilePath = restart.LegacyProcessIDFilePath
	} else {
		// check if restart script is there
		_, err = os.Stat(restart.ScriptPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("the restart container utility script is not present in the container. Please make sure '%s' is in your container and wrapping the entrypoint", restart.ScriptPath)
			}

			return errors.Wrap(err, "cannot access restart helper script")
		}
	}

	// read current active process id
	pgidBytes, err := ioutil.ReadFile(pidFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return errors.Wrap(err, "cannot access restart process id file")
	}

	// convert to int
	pgid, err := strconv.Atoi(strings.TrimSpace(string(pgidBytes)))
	if err != nil {
		return err
	}

	// delete the pid file
	err = os.Remove(pidFilePath)
	if err != nil {
		// someone else was faster than we were
		if os.IsNotExist(err) {
			return nil
		}

		return errors.Wrap(err, "cannot delete restart process id file")
	}

	// kill the process group
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return nil
}

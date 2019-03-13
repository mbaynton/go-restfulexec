# go-restfulexec

A wrapper around Golang's [Gin Web Framework](https://gin-gonic.com/) to let you easily
define web endpoints corresponding to local commandline executions.

To make deploying these things as easy as possible, executables built on this library are
designed to be pretty portable and have minimal configuration requirements. A "code as
configuration" philosophy is used, and of course since it's Go, no OS packages or software
dependencies should be required. Usually you just need to make sure the logfile is writable
to the user you run the process as & firewall as desired.

To develop a new server, you mostly just define the log file for your process, the URLs you
want to listen at and any variable components, the commands your server can execute,
how variable components of the URL map to arguments to those commands, and some regular
expressions the arguments must satisfy to ensure malicous user input does not cause undesired
opeartion. That's all code as configuration; the only real logic you have to write is the
`OutputTransformer`, a function that takes the command's exit code, standard out and error,
and maps them to the http response.

## Example
The below complete server returns the total jobs as reported by `/opt/moab/bin/showq` for a
particular queue ("class") specified by the user in the URL. In this example, the user passes
"nice" as the :class URL component. In the Args slice, `class={{ request "class" }}` passes
class=nice as the third argument to showq, after verifying that it satisfies the corresponding
`ArgValidatorExprs` regular expression.

### Example request/response
```bash
$ curl http://localhost:8080/showq-jobcount/nice
42
$
```

### Code
```golang
package main

import (
	"errors"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mbaynton/go-genericexec"
	"github.com/mbaynton/go-restfulexec"
)

func main() {
	logFile := "/var/log/showq_api.log"
	executables := []restfulexec.RestfulExecConfig{
		restfulexec.RestfulExecConfig{
			GenericExecConfig: genericexec.GenericExecConfig{
				Name:      "showq-jobcount",
				Command:   "/opt/moab/bin/showq",
				Args:      []string{"-r", "-w", "class={{request \"class\"}}"},
				Reentrant: true,
			},
			UrlComponents:     "/:class",
			ArgValidatorExprs: []string{"-r", "-w", "^class=\\w+$"},
			OutputTransformer: jobCountParser,
		},
	}

	gin := restfulexec.NewRestfulExecGin(executables, logFile)
	gin.Run(":8080")
}

func jobCountParser(exitCode int, stdout string, stderr string, c *gin.Context) error {
	if exitCode != 0 {
		return errors.New("showq did not exit 0")
	}

	var jobCountFinder *regexp.Regexp
	var err error
	if jobCountFinder, err = regexp.Compile("Total jobs:\\s*(\\d+)"); err != nil {
		return err
	}
	jobCountExtracts := jobCountFinder.FindStringSubmatch(stdout)
	if jobCountExtracts == nil {
		return errors.New("Total jobs count was not found in showq output.")
	}
	var jobCount int
	if jobCount, err = strconv.Atoi(jobCountExtracts[1]); err != nil {
		return err
	}

	c.JSON(200, jobCount)

	return nil
}
```

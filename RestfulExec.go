package restfulexec

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/gin-gonic/gin"

	"github.com/mbaynton/go-genericexec"
)

type RestfulExecConfig struct {
	genericexec.GenericExecConfig

	UrlComponents     string
	ArgValidatorExprs []string
	OutputTransformer func(int, string, string, *gin.Context) error
}

type RestfulExecGin struct {
	*gin.Engine

	execTaskConfigsByName map[string]RestfulExecConfig
	execManager           *genericexec.GenericExecManager
}

type GinContextToTemplateGetterAdapter struct {
	context *gin.Context
}

type ArgValidatorCallable func(string) (bool, string)

func NewRestfulExecGin(executables []RestfulExecConfig, logFile string) *RestfulExecGin {
	fileOutput, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0640)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: Log file %s: cannot open for writing.\n", logFile)
		os.Exit(1)
	}
	gin.DefaultWriter = fileOutput

	newApp := RestfulExecGin{
		Engine: gin.Default(),

		execTaskConfigsByName: makeExecTaskConfigsMap(executables),
	}
	execLog := log.New(fileOutput, "", log.LstdFlags)
	genericExecMap := newApp.makeGenericExecTaskConfigsMap()
	newApp.execManager = genericexec.NewGenericExecManager(genericExecMap, execLog, nil)

	for _, restfulExec := range executables {
		newApp.GET(fmt.Sprintf("/%s/%s", restfulExec.Name, restfulExec.UrlComponents), func(c *gin.Context) {
			adapter := GinContextToTemplateGetterAdapter{context: c}
			var renderedArgs []string
			if renderedArgs, err = genericexec.RenderArgTemplates(restfulExec.Args, adapter); err != nil {
				c.JSON(500, gin.H{"msg": err})
				return
			}

			// Test each arg against regex or callable.
			for i, untrustedValue := range renderedArgs {
				if i >= len(restfulExec.ArgValidatorExprs) {
					c.JSON(500, gin.H{"msg": fmt.Sprintf("RestfulExecConfig lacks ArgValidator expression for argument %d", i)})
					return
				}
				validateRegexExpr := restfulExec.ArgValidatorExprs[i]
				var regexMatched bool
				if regexMatched, err = regexp.MatchString(validateRegexExpr, untrustedValue); err != nil {
					c.JSON(500, gin.H{"msg": err})
					return
				}
				if regexMatched == false {
					c.JSON(400, gin.H{"msg": fmt.Sprintf("Invalid input (arg %d): \"%s\"", i, untrustedValue)})
					return
				}

				continue

				c.JSON(500, gin.H{"msg": fmt.Sprintf("RestfulExecConfig lacks valid ArgValidator for argument %d. Regular expression strings currently supported.", i)})
				return
			}

			execResultChan := newApp.execManager.RunTask(restfulExec.Name, adapter)
			execResult := <-execResultChan
			err = restfulExec.OutputTransformer(execResult.ExitCode, execResult.StdOut, execResult.StdErr, c)
			if err != nil {
				c.JSON(500, gin.H{"msg": fmt.Sprintf("%v", err)})
				return
			}
		})
	}

	return &newApp
}

func makeExecTaskConfigsMap(execTaskDefns []RestfulExecConfig) map[string]RestfulExecConfig {
	execTaskConfigsByName := make(map[string]RestfulExecConfig, len(execTaskDefns))
	for _, configuredTask := range execTaskDefns {
		execTaskConfigsByName[configuredTask.Name] = configuredTask
	}
	return execTaskConfigsByName
}

func (ctx *RestfulExecGin) makeGenericExecTaskConfigsMap() map[string]genericexec.GenericExecConfig {
	// An unfortunate consequence of Go's lack of true inheritance or understanding of
	// covariance / contravariance is that the ctx.execTaskConfigsByName map is incompatible
	// with the one genericExecManager wants, even though its values are structural subtypes.
	result := make(map[string]genericexec.GenericExecConfig, len(ctx.execTaskConfigsByName))
	for key, value := range ctx.execTaskConfigsByName {
		downcastedCopy := genericexec.GenericExecConfig{
			Name:           value.Name,
			Command:        value.Command,
			Args:           value.Args,
			SuccessMessage: value.SuccessMessage,
			ErrorMessage:   value.ErrorMessage,
			Reentrant:      value.Reentrant,
		}
		result[key] = downcastedCopy
	}
	return result
}

func (ctx GinContextToTemplateGetterAdapter) Get(valueName string) string {
	return ctx.context.Param(valueName)
}

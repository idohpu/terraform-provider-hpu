package provider

import (
	"fmt"
	"strconv"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/rs/zerolog"
)

const (
	errorUint   string = "expected type of %s to be a positive number (uint)"
	errorFloat  string = "expected type of %s to be a float"
	errorString string = "expected type of %s to be string"
)

var logLevels map[string]string
var rootLogger zerolog.Logger

// var macAddressRegex = regexp.MustCompile(`([a-fA-F0-9]{2}:){5}[a-fA-F0-9]{2}`)
// var rxIPconfig = regexp.MustCompile(`ip6?=([0-9a-fA-F:\\.]+)`)

func permissions_check(s1 []string, s2 []string) []string {

	var diff []string

	// loop through s2 and check if each element is in s1
	for _, str2 := range s2 {
		found := false
		for _, str1 := range s1 {
			if str2 == str1 {
				found = true
				break
			}
		}
		if !found {
			diff = append(diff, str2)
		}
	}
	return diff
}

// Further parses a QemuDevice by normalizing types
func adaptDeviceToConf(
	conf map[string]interface{},
	device pxapi.QemuDevice,
) map[string]interface{} {
	// Value type should be one of types allowed by Terraform schema types.
	for key, value := range device {
		// This nested switch is used for nested config like in `net[n]`,
		// where Proxmox uses `key=<0|1>` in string" at the same time
		// a boolean could be used in ".tf" files.
		switch conf[key].(type) {
		case bool:
			switch value := value.(type) {
			// If the key is bool and value is int (which comes from Proxmox API),
			// should be converted to bool (as in ".tf" conf).
			case int:
				sValue := strconv.Itoa(value)
				bValue, err := strconv.ParseBool(sValue)
				if err == nil {
					conf[key] = bValue
				}
			// If value is bool, which comes from Terraform conf, add it directly.
			case bool:
				conf[key] = value
			}
		// Anything else will be added as it is.
		default:
			conf[key] = value
		}
	}

	return conf
}

// Create a sublogger from the rootLogger
// This is helpful as it allows for custom logging level for each component/part of the system.
//
// The loggerName string is used to set the name of the logger in message outputs (as a key-val pair) but
// also as a way to know what we should set the logging level for this sublogger to (info/trace/warn/etc)
func CreateSubLogger(loggerName string) (zerolog.Logger, error) {

	// look to see if there is a default level we should be using
	defaultLevelString, ok := logLevels["_default"]
	if !ok {
		defaultLevelString = "info"
	}

	// set the log level using the default of INFO unless it is override by the logLevels map
	levelString, ok := logLevels[loggerName]
	if !ok {
		levelString = defaultLevelString
	}

	// translate the received log level into the zerolog Level type
	level, err := levelStringToZerologLevel(levelString)
	if err != nil {
		rootLogger.Info().Msgf("Received bad level %v when creating the %v sublogger. Failing back to INFO level.", levelString, loggerName)
		level = zerolog.InfoLevel
	}

	// create the logger
	thisLogger := rootLogger.With().Str("loggerName", loggerName).Logger().Level(level)
	return thisLogger, nil
}

// given a string, return the appropriate zerolog level
func levelStringToZerologLevel(logLevel string) (zerolog.Level, error) {
	conversionMap := map[string]zerolog.Level{
		"panic": zerolog.PanicLevel,
		"fatal": zerolog.FatalLevel,
		"error": zerolog.ErrorLevel,
		"warn":  zerolog.WarnLevel,
		"info":  zerolog.InfoLevel,
		"debug": zerolog.DebugLevel,
		"trace": zerolog.TraceLevel,
	}

	foundResult, ok := conversionMap[logLevel]
	if !ok {
		return zerolog.Disabled, fmt.Errorf("unable to find level %v", logLevel)
	}
	return foundResult, nil
}

// // Returns an unordered list of unique tags
// func RemoveDuplicates(tags *[]pxapi.Tag) *[]pxapi.Tag {
// 	if tags == nil || len(*tags) == 0 {
// 		return nil
// 	}
// 	tagMap := make(map[pxapi.Tag]struct{})
// 	for _, tag := range *tags {
// 		tagMap[tag] = struct{}{}
// 	}
// 	uniqueTags := make([]pxapi.Tag, len(tagMap))
// 	var index uint
// 	for tag := range tagMap {
// 		uniqueTags[index] = tag
// 		index++
// 	}
// 	return &uniqueTags
// }

// func Schema() *schema.Schema {
// 	return &schema.Schema{
// 		Type:     schema.TypeString,
// 		Optional: true,
// 		Computed: true,
// 		ValidateDiagFunc: func(i interface{}, path cty.Path) diag.Diagnostics {
// 			v, ok := i.(string)
// 			if !ok {
// 				return diag.Errorf("expected a string, got: %s", i)
// 			}
// 			for _, e := range *Split(v) {
// 				if err := e.Validate(); err != nil {
// 					return diag.Errorf("tag validation failed: %s", err)
// 				}
// 			}
// 			return nil
// 		},
// 		DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
// 			return String(sortArray(RemoveDuplicates(Split(old)))) == String(sortArray(RemoveDuplicates(Split(new))))
// 		},
// 	}
// }

// func sortArray(tags *[]pxapi.Tag) *[]pxapi.Tag {
// 	if tags == nil || len(*tags) == 0 {
// 		return nil
// 	}
// 	sort.SliceStable(*tags, func(i, j int) bool {
// 		return (*tags)[i] < (*tags)[j]
// 	})
// 	return tags
// }

// func Split(rawTags string) *[]pxapi.Tag {
// 	tags := make([]pxapi.Tag, 0)
// 	if rawTags == "" {
// 		return &tags
// 	}
// 	tagArrays := strings.Split(rawTags, ";")
// 	for _, tag := range tagArrays {
// 		tagSubArrays := strings.Split(tag, ",")
// 		if len(tagSubArrays) > 1 {
// 			tmpTags := make([]pxapi.Tag, len(tagSubArrays))
// 			for i, e := range tagSubArrays {
// 				tmpTags[i] = pxapi.Tag(e)
// 			}
// 			tags = append(tags, tmpTags...)
// 		} else {
// 			tags = append(tags, pxapi.Tag(tag))
// 		}
// 	}
// 	return &tags
// }

// func String(tags *[]pxapi.Tag) (tagList string) {
// 	if tags == nil || len(*tags) == 0 {
// 		return ""
// 	}
// 	for _, tag := range *tags {
// 		tagList += ";" + string(tag)
// 	}
// 	return tagList[1:]
// }

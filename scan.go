package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fatih/structs"
	"github.com/gorilla/mux"
	"github.com/maliceio/go-plugin-utils/database/elasticsearch"
	"github.com/maliceio/go-plugin-utils/utils"
	"github.com/maliceio/malice/utils/clitable"
	"github.com/parnurzeal/gorequest"
	"github.com/urfave/cli"
)

// Version stores the plugin's version
var Version string

// BuildTime stores the plugin's build time
var BuildTime string

var path string

const (
	name     = "windows-defender"
	category = "av"
)

type pluginResults struct {
	ID   string      `json:"id" structs:"id,omitempty"`
	Data ResultsData `json:"windows-defender" structs:"windows-defender"`
}

// WindowsDefender json object
type WindowsDefender struct {
	Results ResultsData `json:"windows-defender"`
}

// ResultsData json object
type ResultsData struct {
	Infected bool   `json:"infected" structs:"infected"`
	Result   string `json:"result" structs:"result"`
	Engine   string `json:"engine" structs:"engine"`
	Updated  string `json:"updated" structs:"updated"`
}

func assert(err error) {
	if err != nil {
		log.WithFields(log.Fields{
			"plugin":   name,
			"category": category,
			"path":     path,
		}).Fatal(err)
	}
}

// AvScan performs antivirus scan
func AvScan(timeout int) WindowsDefender {

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// needs to be run from the /loadlibrary folder
	if err := os.Chdir("/loadlibrary"); err != nil {
		assert(err)
	}
	// will change back to the /malware folder when func returns
	defer os.Chdir("/malware")
	pwd, err := os.Getwd()
	if err != nil {
		assert(err)
	}

	log.WithFields(log.Fields{
		"plugin":   name,
		"category": category,
		"pwd":      pwd,
		"path":     path,
	}).Debug("mpclient paths")

	results, err := ParseWinDefOutput(utils.RunCommand(ctx, "./mpclient", path))
	assert(err)

	return WindowsDefender{
		Results: results,
	}
}

// ParseWinDefOutput convert windef output into ResultsData struct
func ParseWinDefOutput(windefout string, err error) (ResultsData, error) {

	// main(): The map file wasn't found, symbols wont be available
	// main(): usage: ./mpclient [filenames...]
	// root@d9f8dca1d59e:/loadlibrary# ./mpclient /malware/EICAR
	// main(): The map file wasn't found, symbols wont be available
	// main(): Scanning /malware/EICAR...
	// EngineScanCallback(): Scanning input
	// EngineScanCallback(): Threat Virus:DOS/EICAR_Test_File identified.

	if err != nil {
		return ResultsData{}, err
	}

	log.WithFields(log.Fields{
		"plugin":   name,
		"category": category,
		"path":     path,
	}).Debug("Windows Defender Output: ", windefout)

	windef := ResultsData{Infected: false}

	return windef, nil
}

func getUpdatedDate() string {
	if _, err := os.Stat("/opt/malice/UPDATED"); os.IsNotExist(err) {
		return BuildTime
	}
	updated, err := ioutil.ReadFile("/opt/malice/UPDATED")
	assert(err)
	return string(updated)
}

func parseUpdatedDate(date string) string {
	layout := "200601021504"
	t, _ := time.Parse(layout, date)
	return fmt.Sprintf("%d%02d%02d", t.Year(), t.Month(), t.Day())
}

// func updateAV(ctx context.Context) error {
// 	fmt.Println("Updating F-PROT...")
// 	fmt.Println(utils.RunCommand(ctx, "/opt/f-prot/fpupdate"))
// 	// Update UPDATED file
// 	t := time.Now().Format("20060102")
// 	err := ioutil.WriteFile("/opt/malice/UPDATED", []byte(t), 0644)
// 	return err
// }

func printMarkDownTable(windef WindowsDefender) {

	fmt.Println("#### Windows Defender")
	table := clitable.New([]string{"Infected", "Result", "Engine", "Updated"})
	table.AddRow(map[string]interface{}{
		"Infected": windef.Results.Infected,
		"Result":   windef.Results.Result,
		"Engine":   windef.Results.Engine,
		"Updated":  windef.Results.Updated,
	})
	table.Markdown = true
	table.Print()
}

func printStatus(resp gorequest.Response, body string, errs []error) {
	fmt.Println(resp.Status)
}

func webService() {
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/scan", webAvScan).Methods("POST")
	log.Info("web service listening on port :3993")
	log.Fatal(http.ListenAndServe(":3993", router))
}

func webAvScan(w http.ResponseWriter, r *http.Request) {

	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("malware")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "Please supply a valid file to scan.")
		log.Error(err)
	}
	defer file.Close()

	log.Debug("Uploaded fileName: ", header.Filename)

	tmpfile, err := ioutil.TempFile("/malware", "web_")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	data, err := ioutil.ReadAll(file)

	if _, err = tmpfile.Write(data); err != nil {
		log.Fatal(err)
	}
	if err = tmpfile.Close(); err != nil {
		log.Fatal(err)
	}

	// Do AV scan
	path = tmpfile.Name()
	windef := AvScan(60)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(windef); err != nil {
		log.Fatal(err)
	}
}

func main() {

	var elastic string

	cli.AppHelpTemplate = utils.AppHelpTemplate
	app := cli.NewApp()

	app.Name = "windows-defender"
	app.Author = "blacktop"
	app.Email = "https://github.com/blacktop"
	app.Version = Version + ", BuildTime: " + BuildTime
	app.Compiled, _ = time.Parse("20060102", BuildTime)
	app.Usage = "Malice Windows Defender AntiVirus Plugin"
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "verbose, V",
			Usage: "verbose output",
		},
		cli.BoolFlag{
			Name:  "table, t",
			Usage: "output as Markdown table",
		},
		cli.BoolFlag{
			Name:   "callback, c",
			Usage:  "POST results to Malice webhook",
			EnvVar: "MALICE_ENDPOINT",
		},
		cli.BoolFlag{
			Name:   "proxy, x",
			Usage:  "proxy settings for Malice webhook endpoint",
			EnvVar: "MALICE_PROXY",
		},
		cli.StringFlag{
			Name:        "elasitcsearch",
			Value:       "",
			Usage:       "elasitcsearch address for Malice to store results",
			EnvVar:      "MALICE_ELASTICSEARCH",
			Destination: &elastic,
		},
		cli.IntFlag{
			Name:   "timeout",
			Value:  60,
			Usage:  "malice plugin timeout (in seconds)",
			EnvVar: "MALICE_TIMEOUT",
		},
	}
	app.Commands = []cli.Command{
		// {
		// 	Name:    "update",
		// 	Aliases: []string{"u"},
		// 	Usage:   "Update virus definitions",
		// 	Action: func(c *cli.Context) error {
		// 		return updateAV(nil)
		// 	},
		// },
		{
			Name:  "web",
			Usage: "Create a Windows Defender scan web service",
			Action: func(c *cli.Context) error {
				webService()
				return nil
			},
		},
	}
	app.Action = func(c *cli.Context) error {

		var err error

		if c.Bool("verbose") {
			log.SetLevel(log.DebugLevel)
		}

		if c.Args().Present() {
			path, err = filepath.Abs(c.Args().First())
			assert(err)

			if _, err = os.Stat(path); os.IsNotExist(err) {
				assert(err)
			}

			windef := AvScan(c.Int("timeout"))

			// upsert into Database
			elasticsearch.InitElasticSearch(elastic)
			elasticsearch.WritePluginResultsToDatabase(elasticsearch.PluginResults{
				ID:       utils.Getopt("MALICE_SCANID", utils.GetSHA256(path)),
				Name:     name,
				Category: category,
				Data:     structs.Map(windef.Results),
			})

			if c.Bool("table") {
				printMarkDownTable(windef)
			} else {
				windefJSON, err := json.Marshal(windef)
				assert(err)
				if c.Bool("post") {
					request := gorequest.New()
					if c.Bool("proxy") {
						request = gorequest.New().Proxy(os.Getenv("MALICE_PROXY"))
					}
					request.Post(os.Getenv("MALICE_ENDPOINT")).
						Set("X-Malice-ID", utils.Getopt("MALICE_SCANID", utils.GetSHA256(path))).
						Send(string(windefJSON)).
						End(printStatus)

					return nil
				}
				fmt.Println(string(windefJSON))
			}
		} else {
			log.Fatal(fmt.Errorf("Please supply a file to scan with malice/windef"))
		}
		return nil
	}

	err := app.Run(os.Args)
	assert(err)
}

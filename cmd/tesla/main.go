package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/bogosj/tesla"
	"github.com/manifoldco/promptui"
	"github.com/urfave/cli"
	"golang.org/x/oauth2"
)

const (
	mfaPasscodeLength = 6
)

var version = "master" /* passed in by go build */

func main() {
	app := cli.NewApp()
	app.Name = "tesla"
	app.Usage = "Control Tesla cars"
	app.Version = version
	app.Flags = []cli.Flag{
		cli.Int64Flag{
			Name:  "id",
			Usage: "id of vehicle",
		},
		cli.BoolFlag{
			Name:  "zzz, z",
			Usage: "Prevent waking up of vehicle when asleep",
		},
		cli.StringFlag{
			Name:  "token",
			Usage: "Path to token file (default: ~/.tesla-token.json)",
		},
	}
	app.Commands = []cli.Command{
		{
			Name:   "login",
			Usage:  "Login to your account",
			Action: login,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name: "username",
				},
				cli.StringFlag{
					Name: "password",
				},
			},
		},
		{
			Name:   "vehicles",
			Usage:  "List vehicles",
			Action: vehicles,
		},
		{
			Name:   "state",
			Usage:  "Get/set Tesla state",
			Action: state,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name: "wake, w",
				},
			},
		},
		{
			Name:   "charge",
			Usage:  "Get/set charge state",
			Action: charge,
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:  "limit, l",
					Usage: "Set charge limit % (0-100)",
				},
				cli.BoolFlag{
					Name:  "start, s",
					Usage: "Start charging",
				},
				cli.BoolFlag{
					Name:  "stop, x",
					Usage: "Stop charging",
				},
				cli.BoolFlag{
					Name:  "open, o",
					Usage: "Open charge port",
				},
			},
		},
		{
			Name:   "climate",
			Usage:  "Get/set climate state",
			Action: climate,
			Flags: []cli.Flag{
				cli.BoolTFlag{
					Name:  "on",
					Usage: "Set air conditioning on/off",
				},
				cli.Float64Flag{
					Name:  "temp, t",
					Usage: "Set temperature",
				},
				cli.IntFlag{
					Name:  "seatheater",
					Usage: "Set seat heater (0=driver, 1=passenger, ...)",
				},
				cli.IntFlag{
					Name:  "seatlevel",
					Usage: "Set seat heater level (0-3)",
				},
				cli.BoolTFlag{
					Name:  "wheel, w",
					Usage: "Heating steering wheel",
				},
				cli.StringFlag{
					Name:  "window",
					Usage: "Vent or close window",
				},
			},
		},
		{
			Name:   "vehicle",
			Usage:  "Get vehicle state",
			Action: vehicle,
		},
		{
			Name:   "drive",
			Usage:  "Get drive state",
			Action: drive,
		},
		{
			Name:   "action",
			Usage:  "Take an action",
			Action: action,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name: "flash",
				},
				cli.BoolFlag{
					Name: "horn",
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func tokenPath(c *cli.Context) string {
	path := c.String("token")
	if path == "" {
		path = defaultTokenPath()
	}
	return path
}

func defaultTokenPath() string {
	usr, _ := user.Current()
	return filepath.Join(usr.HomeDir, ".tesla-token.json")
}

func connect(c *cli.Context) (*tesla.Client, error) {
	ctx := context.Background()
	client, err := tesla.NewClient(ctx, tesla.WithTokenFile(tokenPath(c)))
	return client, err
}

func getVehicle(c *cli.Context, wake bool) (*tesla.Vehicle, error) {
	client, err := connect(c)
	if err != nil {
		return nil, err
	}

	var vehicle *tesla.Vehicle
	id := c.Int64("id")
	if id != 0 {
		vehicle, err = client.Vehicle(id)
		if err != nil {
			return nil, err
		}
	} else {
		vehicles, err := client.Vehicles()
		if err != nil {
			return nil, err
		}
		if len(vehicles) == 0 {
			return nil, errors.New("No vehicles found")
		}
		vehicle = vehicles[0]
	}

	if wake && vehicle.State == "asleep" {
		if c.GlobalBool("zzz") {
			return vehicle, errors.New("Vehicle requires waking")
		}
		fmt.Print("Waking vehicle...")
		_, err := vehicle.Wakeup()
		if err != nil {
			return nil, err
		}

		// poll for 5 mins
		for retry := 0; retry < 100; retry++ {
			vehicle, err = client.Vehicle(vehicle.ID)
			if err != nil {
				return nil, err
			}
			if vehicle.State == "online" {
				break
			}
			time.Sleep(3 * time.Second)
			fmt.Print(".")
		}

		if vehicle.State == "online" {
			fmt.Println("done")
		} else {
			fmt.Printf("timeout (%s)\n", vehicle.State)
		}
		return vehicle, err
	}
	return vehicle, nil
}

func promptField(c *cli.Context, field string, label string) (string, error) {
	value := c.String("username")
	if value != "" {
		return value, nil
	}
	ui := promptui.Prompt{
		Label:   label,
		Pointer: promptui.PipeCursor,
		Validate: func(s string) error {
			if len(s) == 0 {
				return errors.New("len(s) == 0")
			}
			return nil
		},
	}
	if field == "password" {
		ui.Mask = '*'
	}
	value, err := ui.Run()
	return value, err
}

func login(c *cli.Context) error {
	var err error
	username, err := promptField(c, "username", "Username")
	if err != nil {
		return err
	}
	password, err := promptField(c, "password", "Password")
	if err != nil {
		return err
	}

	verifier, challenge, err := pkce()
	if err != nil {
		return fmt.Errorf("pkce: %w", err)
	}

	ctx := context.Background()
	config := tesla.OAuth2Config

	code, err := (&auth{
		AuthURL: config.AuthCodeURL(oauthState(), oauth2.AccessTypeOffline,
			oauth2.SetAuthURLParam("code_challenge", challenge),
			oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		),
		SelectDevice: selectDevice,
	}).Do(ctx, username, password)
	if err != nil {
		return err
	}

	t, err := config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return fmt.Errorf("exchange: %w", err)
	}

	path := tokenPath(c)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("mkdir all: %w", err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")
	if err := enc.Encode(t); err != nil {
		return fmt.Errorf("json encode: %w", err)
	}
	return nil
}

func selectDevice(ctx context.Context, devices []device) (d device, passcode string, err error) {
	var i int
	if len(devices) > 1 {
		var err error
		i, _, err = (&promptui.Select{
			Label:   "Device",
			Items:   devices,
			Pointer: promptui.PipeCursor,
		}).Run()
		if err != nil {
			return device{}, "", fmt.Errorf("select device: %w", err)
		}
	}
	d = devices[i]

	passcode, err = (&promptui.Prompt{
		Label:   "Passcode",
		Pointer: promptui.PipeCursor,
		Validate: func(s string) error {
			if len(s) != mfaPasscodeLength {
				return errors.New("len(s) != 6")
			}
			return nil
		},
	}).Run()
	if err != nil {
		return device{}, "", err
	}
	return d, passcode, nil
}

func vehicles(c *cli.Context) error {
	client, err := connect(c)
	if err != nil {
		return err
	}

	vehicles, err := client.Vehicles()
	if err != nil {
		return err
	}

	for _, vehicle := range vehicles {
		fmt.Printf("%d: '%s' VIN: %s State: %s\n", vehicle.ID, vehicle.DisplayName, vehicle.Vin, vehicle.State)
	}

	return nil
}

func state(c *cli.Context) error {
	vehicle, err := getVehicle(c, false)
	if err != nil {
		return err
	}

	fmt.Println("Currently:", vehicle.State)

	if c.Bool("wake") {
		fmt.Printf("Waking...")
		vehicle.Wakeup()
		fmt.Println("done")
	}

	return nil
}

func action(c *cli.Context) error {
	vehicle, err := getVehicle(c, true)
	if err != nil {
		return err
	}

	if c.Bool("flash") {
		fmt.Println("Flashing lights...")
		vehicle.FlashLights()
	}

	if c.Bool("horn") {
		fmt.Println("Honking horn...")
		vehicle.HonkHorn()
	}

	return nil
}

func duration(hours float64) string {
	mins := int64(hours*60) - int64(hours)*60
	if hours < 0 {
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%dh%dm", int(hours), mins)
}

func charge(c *cli.Context) error {
	vehicle, err := getVehicle(c, true)
	if err != nil {
		return err
	}

	if c.IsSet("start") && c.IsSet("stop") {
		return errors.New("Only one of --start or --stop should be provided")
	}
	if c.IsSet("start") {
		err := vehicle.StartCharging()
		if err == nil {
			fmt.Println("Starting charging")
		}
		return err
	}
	if c.IsSet("stop") {
		err := vehicle.StopCharging()
		if err == nil {
			fmt.Println("Stopping charging")
		}
		return err
	}
	if c.IsSet("open") {
		err := vehicle.OpenChargePort()
		if err == nil {
			fmt.Println("Opened charge port")
		}
		return err
	}

	if c.IsSet("limit") {
		limit := c.Int("limit")
		if limit < 0 || limit > 100 {
			return errors.New("limit must be between 0 and 100")
		}
		err := vehicle.SetChargeLimit(limit)
		if err == nil {
			fmt.Printf("Set charge limit to: %d%%\n", limit)
		}
		return err
	}

	state, err := vehicle.ChargeState()
	if err != nil {
		return err
	}

	fmt.Printf("State: %d%% (%s)\n", state.BatteryLevel, state.ChargingState)
	if state.FastChargerPresent {
		fmt.Printf("Fast charging: %s %s\n", state.FastChargerType, state.FastChargerBrand)
	}
	if state.BatteryHeaterOn {
		fmt.Println("Battery Heater: On")
	}
	if state.NotEnoughPowerToHeat {
		fmt.Println("Not enough power to heat!")
	}
	fmt.Printf("Range: %.2fmi typical (%.2fmi rated)\n", state.IdealBatteryRange, state.BatteryRange)
	if state.ChargePortDoorOpen {
		fmt.Printf("Charge port door: Open (%s) (Cable: %s)\n", state.ChargePortLatch, state.ConnChargeCable)
	}
	if state.ChargingState != "Disconnected" {
		switch state.ChargingState {
		case "Charging", "Starting":
			fmt.Printf("Charging: %.0fA %.0fV %.0fkW %d-phase %.1fmi/hr\n", state.ChargerActualCurrent, state.ChargerVoltage, state.ChargerPower, state.ChargerPhases, state.ChargeRate)
		case "NoPower":
			fmt.Println("Charging: Ready to Charge")
		default:
			fmt.Printf("Charging: %s\n", state.ChargingState)
		}
		if state.ChargingState != "NoPower" {
			fmt.Printf("Added: +%.2fkWh +%.1fmi (%s to full)\n", state.ChargeEnergyAdded, state.ChargeMilesAddedIdeal, duration(state.TimeToFullCharge))
		}
	}
	fmt.Printf("Current limit: %d/%dA Charge limit: %d/%d%%\n", state.ChargeCurrentRequest, state.ChargeCurrentRequestMax, state.ChargeLimitSoc, state.ChargeLimitSocMax)

	return nil
}

func formatTemp(temp *float64) string {
	if temp != nil {
		return fmt.Sprintf("%.1f", *temp)
	}
	return "unknown"
}

func climate(c *cli.Context) error {
	window := c.String("window")
	if window != "" && window != "close" && window != "vent" {
		return errors.New("window should be 'vent' or 'close'")
	}

	vehicle, err := getVehicle(c, true)
	if err != nil {
		return err
	}

	if c.IsSet("on") {
		if c.BoolT("on") {
			err = vehicle.StartAirConditioning()
			if err != nil {
				return err
			}
			fmt.Println("Started Air Conditioning")
		} else {
			err = vehicle.StopAirConditioning()
			if err != nil {
				return err
			}
			fmt.Println("Stopped Air Conditioning")
		}
	}

	if c.IsSet("temp") {
		temp := c.Float64("temp")
		if temp < 15 || temp > 20 {
			return errors.New("temp must be between 15 and 20")
		}
		err := vehicle.SetTemperature(temp, temp)
		if err != nil {
			return err
		}
		fmt.Printf("Set temperature to: %s\n", formatTemp(&temp))

		err = vehicle.StartAirConditioning()
		if err != nil {
			return err
		}
		fmt.Println("Started Air Conditioning")
	}

	if c.IsSet("seatheater") {
		heater := c.Int("seatheater")
		level := c.Int("seatlevel")
		err := vehicle.SetSeatHeater(heater, level)
		if err != nil {
			return err
		}
		fmt.Printf("Set seat heater %d to %d\n", heater, level)
	}

	if c.IsSet("wheel") {
		on := c.BoolT("wheel")
		err := vehicle.SetSteeringWheelHeater(on)
		if err != nil {
			return err
		}
		fmt.Printf("Set steering wheel heater to: %t\n", on)
	}

	if window != "" {
		driveState, _ := vehicle.DriveState()
		err := vehicle.WindowControl(window, driveState.Latitude, driveState.Longitude)
		if err != nil {
			return err
		}
		fmt.Printf("Set windows to: %s\n", window)
	}

	state, err := vehicle.ClimateState()
	if err != nil {
		return err
	}
	fmt.Printf("Temperature inside: %.1f outside: %.1f\n", state.InsideTemp, state.OutsideTemp)

	return nil
}

func vehicle(c *cli.Context) error {
	vehicle, err := getVehicle(c, true)
	if err != nil {
		return err
	}

	state, err := vehicle.VehicleState()
	if err != nil {
		return err
	}
	fmt.Printf("%#v\n", *state)

	return nil
}

func drive(c *cli.Context) error {
	vehicle, err := getVehicle(c, true)
	if err != nil {
		return err
	}

	state, err := vehicle.DriveState()
	if err != nil {
		return err
	}
	if state.ShiftState != nil {
		fmt.Printf("Shift state: %v", state.ShiftState)
	}
	fmt.Println()
	fmt.Printf("%#v\n", *state)

	return nil
}

# Tesla CLI

Command line tool for querying and controlling your Tesla Model S/3/X/Y vehicle.

## Login

    $ tesla login

This generates your login token and puts it in ~/.tesla-token.json.

This will prompt for your username and password. You can provide `--username` and `--password` on the command line but this is generally not recommended as insecure.

## Usage

To list vehicles:

    $ tesla vehicles

To get the state (online/sleeping) of the first vehicle:

    $ tesla state

The above commands operate without waking a sleeping vehicle. Anything below will wake the vehicle, if necessary, to
get the information.

To get the charge state:

    $ tesla charge

### Power saving

Running a command that requires the vehicle to be awake, would normally wake it up if asleep, which can be undesirable as this causes more 'vampire drain' if done throughout the day when normally the vehicle would save power by sleeping.

Instead you can run commands with `--zzz` to prevent wake up in these situations, eg:

    $ tesla -z charge

### Multiple vehicles

By default we assume all commands are applied to the first vehicle. If you're lucky enough to have more than one, then
run `tesla vehicles` to get the ids for your vehicles and then subsequent commands with `tesla --id <number>` to target a particular vehicle.

## Credits

Thank you to [Tim Dorr](https://github.com/timdorr) who did the heavy lifting to document the Tesla API and also created the [model-s-api Ruby Gem](https://github.com/timdorr/model-s-api).

Thank you to [jsgoecke](https://github.com/jsgoecke) for the original Go telsa library and to [bogosj](https://github.com/bogosj) for forking and maintaining it.

## Copyright & License

Copyright (c) 2021 Barnaby Gray.

Released under the terms of the MIT license. See LICENSE for details.
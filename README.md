# SerialToTCPBridgeProtocol
 An error tolerant serial UART to TCP connection, raw data bridge.

PC side service written in Go that listens on COM ports for serial clients.  
This is meant to bridge the gap between tcp connections and serial devices using UART/RS-232/Virtual COM over USB, etc.  
Clients implementing the protocol have a tcp like api that they can use to make connections to real servers.  
The goal of the project is to have the means to connect the simplest and cheapest devices to the internet, albeit indirectly.  
For now, I use this to connect microcontrollers on development boards to servers running on localhost through the Virtual COM port over USB, without requiring any Ethernet/Wi-Fi hardware.


See [STM32SerialToTCPBridgeClient](https://github.com/RoanBrand/STM32SerialToTCPBridgeClient) for an example of a client, written in c, that connects to a MQTT broker from a STM32 Nucleo F334R8 development board (ARM Cortex-M4).


#### Details
- The Go service opens a real TCP connection to a set destination on behalf of the serial client.
- The client specifies the destination IPv4 address and port.
- The protocol provides the app an in order, duplicates free and error checked byte stream by adding a CRC32 and simple retry mechanism. See [this](https://en.wikibooks.org/wiki/Serial_Programming/Error_Correction_Methods) for background.
- The service forwards traffic bi-directionally, as long as tcp connection is open and serial line is good.

#### Future plans
- Add config. Turn into OS service. 
- Add ping option to periodically test serial line and drop upstream connection if timeout.
- Separate connection and com interface concepts in code in preparation of 'multiple connections per client to servers' capability.
- Unit tests.
- PC side service to listen to all COM ports and concurrently spawn new connections for clients
- Create a Arduino lib/client that extends the [Arduino Client class](https://www.arduino.cc/en/Reference/ClientConstructor) so that libraries for existing Ethernet/Wi-Fi shields can theoretically work.

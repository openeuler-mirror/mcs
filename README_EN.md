# mcs

## Overview

Industrial control equipment, aerospace systems, robotics, and intelligent vehicles increasingly require rich features and robust ecosystems, which in turn demands higher real-time performance, reliability, and security. Consequently, relying on a single OS to host all functionalities poses growing challenges. To address these scenarios, we propose the **Mixed Criticality System (MCS)**. It enables the deployment of multiple OSs on a single system-on-chip (SoC), simultaneously offering the service management capabilities of Linux alongside the deterministic, real-time, and reliable key capabilities provided by a real-time OS (RTOS).

## Software Architecture

**mcs_km**: Provides the necessary kernel modules for OpenAMP, supporting functionalities such as Client OS boot, dedicated interrupt handling (transmission and reception), and reserved memory management.

**mica**: Contains the `mica` command-line tool and the `micad` daemon. Users manage the RTOS lifecycle—including start, stop, and status retrieval—via the `mica` command. The `micad` daemon listens for requests from `mica` and executes the corresponding operations. Additionally, `micad` handles service registration across different instances.

**library**: Contains the implementations of the lifecycle management framework and the service framework.

**rtos**: Provides sample image files, where each ELF image is associated with a corresponding configuration file.

## Build and Installation

MCS supports two build and installation methods:

1. **Integrated build**: Build an image that includes MCS features via Yocto.

   Integrated builds for MCS have already been implemented in openEuler Embedded releases, supporting one-click generation of QEMU, Raspberry Pi, and other images containing MCS. The integrated build process relies on the `oebuild` tool.

2. **Standalone build**: Compile each MCS component individually, and then install them into the operating environment along with their dependency libraries (`libmetal.so*`, `libopen_amp.so*`, `libsysfs.so*`).

   After building an MCS-enabled SDK using the integrated build method, you can use the SDK to rapidly develop MCS based on the following steps:

   1. Install the SDK and configure the SDK environment variables.

   2. Cross-compile the kernel modules `mcs_km.ko` (for bare-metal/heterogeneous deployment) and `xen-mcsback.ko` (for Xen deployment). The compilation method is as follows:

      ```shell
      cd mcs_km
      make
      ```

      Copy the compiled `.ko` files to the `/lib/modules/$(uname -r)` directory in the operating environment, and run the following commands (using `mcs_km` as an example):

      ```shell
      depmod
      echo "mcs_km" > /etc/modules-load.d/mcs_km.conf
      ```

   3. Cross-compile `micad`. The compilation method is as follows:

      ```shell
      cmake -S . -B build
      ## Note: To build a binary with debugging information, configure CMAKE_BUILD_TYPE, for example:
      ## cmake -S . -B build -DCMAKE_BUILD_TYPE=Debug

      cd build
      make
      ```

      Copy the compiled `micad` binary to the `/usr/bin` directory in the operating environment. To enable autostart for `micad`, you can manually install the initialization script or systemd service provided in the `mica/micad/init` directory of this repository.

   4. Install the `mica` command-line tool and its configuration files:

      ```shell
      scp mica/micactl/mica.py target@xxx:/usr/bin/mica

      scp rtos/arm64/*.conf target@xxx:/etc/mica/
      ```

   5. Retrieve the dependency libraries—including `libmetal`, `libopen_amp`, and `libsysfs`—from the SDK sysroots using the following method:

      ```shell
      # Assuming the SDK installation path is /opt/openeuler/sdk
      cd /opt/openeuler/sdk/sysroot
      find . -name libmetal.so*
      find . -name libopen_amp.so*
      find . -name libsysfs.so*
      ```

      Copy the retrieved `.so` files to the `/usr/lib64` directory in the operating environment.

## Instructions

Currently, MCS supports multiple platforms including qemu-arm64, Raspberry Pi 4B, Hi3093, OK3568, and x86 industrial PCs.

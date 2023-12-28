#!/bin/bash

#######################################################################
##- @Copyright (C) Huawei Technologies., Ltd. 2023. All rights reserved.
# - mcs licensed under the Mulan PSL v2.
# - You can use this software according to the terms and conditions of the Mulan PSL v2.
# - You may obtain a copy of Mulan PSL v2 at:
# -     http://license.coscl.org.cn/MulanPSL2
# - THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND, EITHER EXPRESS OR
# - IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT, MERCHANTABILITY OR FIT FOR A PARTICULAR
# - PURPOSE.
# - See the Mulan PSL v2 for more details.
##- @Description: Used to create a qemu dtb file for mica
##- @Author: hanzongcheng
#######################################################################

JAILHOUSE_ON=""
TMP_DTS=".tmp.qemu.dts"
OUTPUT_DTB="qemu.dtb"

usage()
{
	cat <<-END >&2
	Note:
	    Execute this script will delete ${TMP_DTS} in current directory
	    and overwrite ${OUTPUT_DTB}, so please be careful with backups.

	Usage: $0 ARCHITECTURE [-f FEATURES]

	  Available ARCHITECTURE:
	    qemu-a53                # create a dtb for qemu_cortex_a53

	  Available FEATURES:
	    jailhouse               # create a dtb for jailhouse root cell

	  eg,
	    # create a dtb for qemu_cortex_a53
	        $0 qemu-a53

	    # create a dtb for qemu_cortex_a53 to support jailhouse
	        $0 qemu-a53 -f jailhouse
	END
	exit
}

die() {
	echo >&2 "$@"
	exit 1
}

check_commands()
{
	# Depends: qemu-system-aarch64, dtc
	for COMMAND in qemu-system-aarch64 dtc; do
		if ! command -v $COMMAND > /dev/null; then
			echo "$COMMAND not found"
			return 1
		fi
	done

	return 0
}

check_opt()
{
	ARGS=`getopt -o hf: --long help,feature: -n "$0" -- "$@"`
	if [ $? != 0 ]; then
		usage
	fi

	eval set -- "${ARGS}"

	while true
	do
		case "$1" in
			-h|--help)
				usage
				;;
			-f|--feature)
				if [ "$2" = "jailhouse" ]; then
					JAILHOUSE_ON="on"
					OUTPUT_DTB="qemu-jailhouse.dtb"
				else
					die "Invalid feature: ${2}. See '--help' option output for help."
				fi
				shift 2
				;;
			--)
				shift
				break
				;;
			*)
				break
				;;
		esac
	done
}


# ${1} - dts
append_rproc_node()
{
	cat <<-END >> $1

	        reserved-memory {
	            #address-cells = <0x02>;
	            #size-cells = <0x02>;
	            ranges;
	
	            ivshmem_pci@6fffc000 {
	                reg = <0x00 0x6fffc000 0x00 0x4000>;
	                no-map;
	            };
	
	            client_os_reserved: client_os_reserved@7a000000 {
	                reg = <0x00 0x7a000000 0x00 0x4000000>;
	                no-map;
	            };
	
	            client_os_dma_memory_region: client_os-dma-memory@70000000 {
	                compatible = "shared-dma-pool";
	                reg = <0x00 0x70000000 0x00 0x100000>;
	                no-map;
	            };
	        };
	
	        mcs-remoteproc {
	            compatible = "oe,mcs_remoteproc";
	            memory-region = <&client_os_dma_memory_region>,
	                            <&client_os_reserved>;
	        };
	    };
	END
}

# ${1} - qemu
create_qemu_dtb()
{
	QEMU=$1
	QEMU_EXTRA_ARGS=" \
			-cpu cortex-a53 \
			-smp 4 \
			-m 2G \
			-nographic \
			-M dumpdtb=${OUTPUT_DTB}"

	if [ -n "${JAILHOUSE_ON}" ]; then
		# For jailhouse, psci method: smc
		QEMU_EXTRA_ARGS="${QEMU_EXTRA_ARGS} -machine virt,gic-version=3,virtualization=on,its=off"
	else
		QEMU_EXTRA_ARGS="${QEMU_EXTRA_ARGS} -machine virt,gic-version=3"
	fi

	${QEMU} ${QEMU_EXTRA_ARGS} 2>/dev/null

	if [ -f "${OUTPUT_DTB}" ]; then
		dtc -I dtb -O dts -o ${TMP_DTS} ${OUTPUT_DTB} 2>/dev/null
	else
		die "Cannot create ${OUTPUT_DTB}"
	fi

	if [ ! -f "${OUTPUT_DTB}" ]; then
		die "Cannot create ${TMP_DTS}"
	fi
	# append reserved-memory
	sed -i '$ d' ${TMP_DTS}
	append_rproc_node ${TMP_DTS}

	dtc -I dts -O dtb -o ${OUTPUT_DTB} ${TMP_DTS} 2>/dev/null
	rm ${TMP_DTS}
}

case "$1" in
	qemu-a53)
		if ! check_commands; then
			echo "Depending on: qemu-system-aarch64, dtc. Please install those."
			exit
		fi
		shift
		check_opt $@
		create_qemu_dtb qemu-system-aarch64
		echo "${OUTPUT_DTB} successfully created"
		;;
	*)
		usage
esac

exit 0

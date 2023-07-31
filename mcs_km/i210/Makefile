KERNELDIR := ${KERNEL_SRC}
CURRENT_PATH := $(shell pwd)

MK_DIR := $(abspath $(lastword $(MAKEFILE_LIST)))
PWD_DIR := $(shell dirname $(MK_DIR))
PATH_TO_ANOTHER := $(abspath $(PWD_DIR)/../)
KBUILD_EXTRA_SYMBOLS += $(PATH_TO_ANOTHER)/Module.symvers

target := eth_i210
obj-m := $(target).o

$(target)-objs := i210_main.o
$(target)-objs += i210_drv.o ## 非module模块的.c文件

build := kernel_modules

kernel_modules:
	$(MAKE) -C $(KERNELDIR) M=$(CURRENT_PATH) modules
clean:
	$(MAKE) -C $(KERNELDIR) M=$(CURRENT_PATH) clean
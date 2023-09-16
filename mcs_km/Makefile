obj-m := mcs_km.o
obj-m += mcs_remoteproc.o

SRC := $(shell pwd)
EXTRA_CFLAGS += -I$(KERNEL_SRC)/drivers/remoteproc
EXTRA_CFLAGS += -I$(src)/include

ifneq ($(JAILHOUSE_SRC),)
	KBUILD_EXTRA_SYMBOLS += $(JAILHOUSE_SRC)/Module.symvers
	obj-m += mcs_ivshmem.o
endif

all:
	$(MAKE) -C $(KERNEL_SRC) M=$(SRC)

modules_install:
	$(MAKE) -C $(KERNEL_SRC) M=$(SRC) modules_install

clean:
	rm -f *.o *~ core .depend .*.cmd *.ko *.mod.c *.mod
	rm -f Module.markers Module.symvers modules.order
	rm -rf .tmp_versions Modules.symvers

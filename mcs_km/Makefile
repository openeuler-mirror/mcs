obj-m := mcs_km.o
ifeq ($(CONFIG_XEN),y)
	obj-m := xen-mcsback.o
else
	# Exactly one pedestal backend must be selected.  The recipe passes
	# all three variables, using 'y' for the selected backend and an
	# empty value for the others.  For standalone builds fall back to
	# aarch64 only when no backend was requested, so that selecting a
	# single non-default backend does not require clearing the rest.
	CONFIG_MCS_PED_AARCH64 ?=
	CONFIG_MCS_PED_HISI_RISCV ?=
	CONFIG_MCS_PED_STM32MP2 ?=

	MCS_PED_COUNT := $(words $(filter y,$(CONFIG_MCS_PED_AARCH64) $(CONFIG_MCS_PED_HISI_RISCV) $(CONFIG_MCS_PED_STM32MP2)))
	ifeq ($(MCS_PED_COUNT),0)
		CONFIG_MCS_PED_AARCH64 := y
		MCS_PED_COUNT := 1
	endif
	ifneq ($(MCS_PED_COUNT),1)
$(error Exactly one MCS pedestal backend must be selected, got '$(MCS_PED_COUNT)')
	endif

	mcs_km-y := mcs_km_core.o mcs_km_common.o
	mcs_km-$(CONFIG_MCS_PED_AARCH64) += mcs_km_aarch64.o
	mcs_km-$(CONFIG_MCS_PED_HISI_RISCV) += mcs_km_hisi_riscv.o
	mcs_km-$(CONFIG_MCS_PED_STM32MP2) += mcs_km_stm32mp2.o

	ccflags-$(CONFIG_MCS_PED_AARCH64) += -DMCS_PEDESTAL_AARCH64
	ccflags-$(CONFIG_MCS_PED_HISI_RISCV) += -DMCS_PEDESTAL_HISI_RISCV
	ccflags-$(CONFIG_MCS_PED_STM32MP2) += -DMCS_PEDESTAL_STM32MP2
endif

SRC := $(shell pwd)

all:
	$(MAKE) -C $(KERNEL_SRC) M=$(SRC)

modules_install:
	$(MAKE) -C $(KERNEL_SRC) M=$(SRC) modules_install

clean:
	rm -f *.o *~ core .depend .*.cmd *.ko *.mod.c *.mod
	rm -f Module.markers Module.symvers modules.order
	rm -rf .tmp_versions Modules.symvers

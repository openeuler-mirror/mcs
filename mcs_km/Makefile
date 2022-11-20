obj-m := mcs_km.o

all:
	$(MAKE) -C $(KERNEL_SRC_DIR) M=$(PWD) modules
clean:
	$(MAKE) -C $(KERNEL_SRC_DIR) M=$(PWD) clean

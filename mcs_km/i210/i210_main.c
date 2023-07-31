#include <linux/types.h>
#include <linux/module.h>
#include <linux/pci.h>
#include "i210_eth.h"

const char g_i210_driver_name[] = "i210_eth";
#define E1000_DEV_ID_I210_COPPER        0x1533
#define E1000_DEV_ID_I210_FIBER         0x1536
#define E1000_DEV_ID_I210_SERDES        0x1537
#define E1000_DEV_ID_I210_SGMII         0x1538
#define E1000_DEV_ID_I210_COPPER_FLASHLESS  0x157B
#define E1000_DEV_ID_I210_SERDES_FLASHLESS  0x157C

static struct pci_device_id i210_pci_tbl[] = {
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_COPPER) },
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_FIBER) },
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_SERDES) },
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_SGMII) },
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_COPPER_FLASHLESS) },
    { PCI_VDEVICE(INTEL, E1000_DEV_ID_I210_SERDES_FLASHLESS) },
    { 0, } /* required last entry */
};

MODULE_DEVICE_TABLE(pci, i210_pci_tbl);

u32 g_dev_num_i210 = 0;
PCI_DEV_INFO_S g_mac_dev_info[I210_DEV_MAX_NUM] = { {0} };

const PCI_DEV_INFO_S* get_mac_dev_info(u32 dev_id)
{
    if ((dev_id >= g_dev_num_i210) || (dev_id >= I210_DEV_MAX_NUM)) {
        return NULL;
    }
    return &(g_mac_dev_info[dev_id]);
}

void mac_dev_func_hook(u32 dev_id, pfn_free_dma_map_t fn_free_dma_map)
{
    if ((dev_id >= g_dev_num_i210) || (dev_id >= I210_DEV_MAX_NUM)) {
        return;
    }
    g_mac_dev_info[dev_id].pfn_free_dma_map = fn_free_dma_map;
}

static int i210_probe(struct pci_dev *pdev, const struct pci_device_id *ent)
{
    int err;
    u8 __iomem *bar_addr;
    u32 bar_size;
    void __iomem *bar;
    unsigned char *pmac_addr;
    const char *eth_dev_name = pci_name(to_pci_dev(&pdev->dev));
    printk(KERN_INFO "%s probe func: eth_dev_name:%s\n", g_i210_driver_name,
        eth_dev_name);
    if (g_dev_num_i210 >= I210_DEV_MAX_NUM) {
        return -E2BIG;
    }
    g_dev_num_i210++;

    err = pci_enable_device(pdev);
    if (err != 0) {
        printk(KERN_INFO "pci enable device failed\n");
        return err;
    }

    err = dma_set_mask_and_coherent(&pdev->dev, DMA_BIT_MASK(64));
    if (err != 0) {
        err = dma_set_mask_and_coherent(&pdev->dev, DMA_BIT_MASK(32));
        if (err) {
            dev_err(&pdev->dev, "No usable DMA configuration, aborting\n");
            goto err_dma;
        }
    }

    err = pci_request_regions(pdev, g_i210_driver_name);
    if (err)
        goto err_dma;

    pci_set_master(pdev);
    pci_save_state(pdev);

    /* 打印设备的BAR地址和大小 */
    bar_addr = (u8 __iomem *)pci_resource_start(pdev, 0);
    bar_size = pci_resource_len(pdev, 0);
    printk(KERN_INFO "i210: BAR0 address = 0x%llx, size = %u", (u64)bar_addr,
        bar_size);

    /* 映射BAR空间 */
    bar = pci_iomap(pdev, 0, bar_size);
    if (!bar) {
        printk(KERN_INFO "i210: Failed to map BAR0");
        err = -ENOMEM;
        goto err_pci_reg;
    }

    g_mac_dev_info[g_dev_num_i210 - 1].dev_id = g_dev_num_i210 - 1;
    g_mac_dev_info[g_dev_num_i210 - 1].pdev = (struct pci_dev *)pdev;
    g_mac_dev_info[g_dev_num_i210 - 1].io_phy = bar_addr;
    g_mac_dev_info[g_dev_num_i210 - 1].io_virt = bar;
    pci_set_drvdata(pdev, &(g_mac_dev_info[g_dev_num_i210 - 1]));
    printk(KERN_INFO "eth_i210[%u]: io_addr:pa=0x%llx va=0x%llx\n",
        (g_dev_num_i210 - 1),
        (u64)g_mac_dev_info[g_dev_num_i210 - 1].io_phy,
        (u64)g_mac_dev_info[g_dev_num_i210 - 1].io_virt);

    /* 尝试打印网卡的MAC地址 */
    pmac_addr = (unsigned char*)(bar + 0x5400);
    printk(KERN_INFO "i210: MAC address is %02x:%02x:%02x:%02x:%02x:%02x\n",
        pmac_addr[0], pmac_addr[1], pmac_addr[2], pmac_addr[3], pmac_addr[4],
        pmac_addr[5]);

    /* 初始化mac_dev_info */
    err = mac_dev_info_init();
    if (err != 0) {
        printk(KERN_INFO "mac_dev_info_init fail, ret:0x%x", err);
        goto err_pci_reg;
    }

    return 0;

err_pci_reg:
    pci_release_regions(pdev);
err_dma:
    pci_disable_device(pdev);
    return err;
}

static void i210_remove(struct pci_dev *pdev)
{
    PCI_DEV_INFO_S *mac_dev;
    printk(KERN_INFO "remove %s\n", g_i210_driver_name);

    mac_dev = (PCI_DEV_INFO_S *)pci_get_drvdata(pdev);
    if (mac_dev->io_virt) {
        pci_iounmap(pdev, mac_dev->io_virt);
    }
    if (mac_dev->pfn_free_dma_map != NULL) {
        mac_dev->pfn_free_dma_map(mac_dev->dev_id);
    }
    pci_release_regions(pdev);
    pci_disable_device(pdev);
}

static struct pci_driver i210_driver = {
    .name = g_i210_driver_name,
    .id_table = i210_pci_tbl,
    .probe = i210_probe,
    .remove = i210_remove,
};

static int __init i210_init(void)
{
    printk(KERN_INFO "Hello, i210_init!\n");
    return pci_register_driver(&i210_driver);
}

static void __exit i210_exit(void)
{
    pci_unregister_driver(&i210_driver);
    printk(KERN_INFO "Byebye!");
}

module_init(i210_init);
module_exit(i210_exit);

MODULE_AUTHOR("OpenEuler Embedded");
MODULE_LICENSE("Dual BSD/GPL");
MODULE_DESCRIPTION("Intel i210 Gigabit Ethernet driver");
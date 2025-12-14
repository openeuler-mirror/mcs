import os
import pty
from pathlib import Path

from numpy.random.mtrand import f


def run_zephyr_mock(id: str):
    # 1. pty master-slave pair
    # master_fd  (mocking rpmsg)
    # slave_fd   (/dev/pts/N)
    master_fd, slave_fd = pty.openpty()

    slave_name = os.ttyname(slave_fd)
    tty_dir = Path("/tmp/mica")
    tty_dir.mkdir(parents=True, exist_ok=True)
    link_name=tty_dir / f"ttyRPMSG_{str}_0"
    os.symlink(slave_name, link_name)
    print(f"【RPMSG Proxy】Virt pty established : {link_name} -> {slave_name}")
    print("【Zephyr】Ready")

    os.set_blocking(master_fd, True)

    prompt = b"uart:~$ "
    os.write(master_fd, b"\r\n*** Booting Zephyr OS build v3.x.x ***\r\n")
    os.write(master_fd, prompt)

    input_buffer = b""

    try:
        while True:
            try:
                data = os.read(master_fd, 1024)
            except OSError:
                break

            if not data:
                break

            # (Echo)
            os.write(master_fd, data)

            input_buffer += data

            # Detecting CR/LF
            if b'\r' in data or b'\n' in data:
                os.write(master_fd, b"\r\n")

                cmd = input_buffer.decode(errors='ignore').strip()
                input_buffer = b""

                if cmd == "help":
                    resp = (
                        "Please press the <Tab> button to see all available commands.\r\n"
                        "hello     : say hello\r\n"
                        "kernel    : kernel commands\r\n"
                        "device    : device commands\r\n"
                        "history   : command history\r\n"
                    )
                    os.write(master_fd, resp.encode())
                elif cmd == "hello":
                    os.write(master_fd, b"Hello from Zephyr!\r\n")
                elif cmd == "":
                    pass
                else:
                    msg = f"Error: {cmd} not found\r\n"
                    os.write(master_fd, msg.encode())

                os.write(master_fd, prompt)

    except KeyboardInterrupt:
        print("\n【RPMSG Proxy】Stopped")
    finally:
        os.unlink(link_name)
        os.close(master_fd)
        os.close(slave_fd)

if __name__ == "__main__":
    import sys
    if len(sys.argv) < 2:
      raise os.error("zephyr name can not be empty!")
    name = sys.argv[1]
    run_zephyr_mock(name)

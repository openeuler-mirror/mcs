#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# SPDX-License-Identifier: MulanPSL-2.0

from argparse import ArgumentParser
from configparser import ConfigParser
import argcomplete
import sys
import os
import socket
import struct

__version__ = "0.0.1"

class mica_create_msg:
    def __init__(self, cpu, name, path):
        self.cpu = cpu
        self.name = name
        self.path = path

    def pack(self):
        # max name length: 32
        # max path length: 128
        return struct.pack('I32s128s', self.cpu, self.name.encode(), self.path.encode())


class mica_socket:
    def __init__(self, socket_path):
        self.socket_path = socket_path
        self.socket = None

    def __enter__(self):
        self.connect()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.disconnect()

    def connect(self):
        self.socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.socket.connect(self.socket_path)

    def disconnect(self):
        if self.socket:
            self.socket.close()
            self.socket = None

    def send_msg(self, msg):
        if self.socket:
            self.socket.sendall(msg)
        else:
            print(f'Failed to connect to {self.socket_path}')

    def recv(self, buffer_size=512, timeout=5):
        self.socket.settimeout(timeout)
        try:
            response_buffer = ''
            while True:
                chunk = self.socket.recv(buffer_size).decode()
                response_buffer += chunk
                if 'MICA-FAILED' in response_buffer:
                    parts = response_buffer.split('MICA-FAILED')
                    msg = parts[0].strip()
                    print('Error occurred!')
                    if msg:
                        print(msg)
                    print("Please see system log ('/var/log/message' or 'journal -u micad') for details.")
                    return 'MICA-FAILED'
                elif 'MICA-SUCCESS' in response_buffer:
                    parts = response_buffer.split('MICA-SUCCESS')
                    msg = parts[0].strip()
                    if msg:
                        print(msg)
                    return 'MICA-SUCCESS'
                elif len(chunk) == 0:
                    break
            return None
        except socket.timeout:
            print('Timeout while waiting for micad response!')
            return None


def send_create_msg(config_file: str) -> None:
    if not os.path.isfile(config_file):
        print(f"Configuration file '{config_file}' not found.")
        exit(1)

    if not os.path.exists('/run/mica/mica-create.socket'):
        print('Error occurred! Please check if micad is running.')
        exit(1)

    parser = ConfigParser()
    parser.read(config_file)
    auto_boot = False
    try:
        cpu = int(parser.get('Mica', 'CPU'))
        name = parser.get('Mica', 'Name')
        path = parser.get('Mica', 'ClientPath')
        if parser.has_option('Mica', 'AutoBoot'):
            auto_boot = parser.getboolean('Mica', 'AutoBoot')
    except Exception as e:
        print(f'Error parsing {config_file}: {e}')
        return

    msg = mica_create_msg(cpu, name, path)
    print(f'Creating {name}...')

    with mica_socket('/run/mica/mica-create.socket') as socket:
        socket.send_msg(msg.pack())
        response = socket.recv()
        if response == 'MICA-SUCCESS':
            print(f'Successfully created {name}!')
        elif response == 'MICA-FAILED':
            print(f'Create {name} failed!')
            exit(1)

    if auto_boot:
        print(f'starting {name}...')
        ctrl_socket = f'/run/mica/{name}.socket'
        with mica_socket(ctrl_socket) as socket:
            command = 'start'
            socket.send_msg(command.encode())
            response = socket.recv()
            if response == 'MICA-SUCCESS':
                print(f'start {name} successfully!')
            elif response == 'MICA-FAILED':
                print(f'start {name} failed!')


def query_status() -> None:
    if not os.path.exists('/run/mica/mica-create.socket'):
        print('Error occurred! Please check if micad is running.')
        exit(1)

    output = f"{'Name':<30}{'Assigned CPU':<20}{'State':<20}{'Service'}"
    print(output)
    directory = '/run/mica'
    files = os.listdir(directory)

    for filename in files:
        if filename == 'mica-create.socket':
            continue
        if filename.endswith('.socket'):
            socket_path = os.path.join(directory, filename)
            with mica_socket(socket_path) as socket:
                command = 'status'
                socket.send_msg(command.encode())
                response = socket.recv()
                if response == 'MICA-FAILED':
                    print(f'Query {name} status failed!')


def send_ctrl_msg(command: str, client: str) -> None:
    ctrl_socket = f'/run/mica/{client}.socket'
    if not os.path.exists(ctrl_socket):
        print(f"Cannot find {client}. Please run 'mica create <config>' to create it.")
        exit(1)

    with mica_socket(ctrl_socket) as socket:
        socket.send_msg(command.encode())
        response = socket.recv()
        if response == 'MICA-SUCCESS':
            print(f'{command} {client} successfully!')
        elif response == 'MICA-FAILED':
            print(f'{command} {client} failed!')


def create_parser() -> ArgumentParser:
    parser = ArgumentParser(
        prog='mica',
        description='Query or send control commands to the micad.'
    )

    subparsers = parser.add_subparsers(dest='command', help='the command to execute')

    # Create command
    create_parser = subparsers.add_parser('create', help='Create a new mica client')
    create_parser.add_argument('config', help='the configuration file of mica client')

    # Start command
    start_parser = subparsers.add_parser('start', help='Start a client')
    start_parser.add_argument('client', help='the name of the client')

    # Stop command
    stop_parser = subparsers.add_parser('stop', help='Stop a client')
    stop_parser.add_argument('client', help='the name of the client')

    # Query status
    status_parser = subparsers.add_parser('status', help='query the mica client status')

    argcomplete.autocomplete(parser)
    return parser


def main() -> None:
    parser = create_parser()
    args = parser.parse_args(args=None if sys.argv[1:] else ['--help'])

    if args.command == 'create':
        send_create_msg(args.config)
    elif args.command == 'start':
        print(f'starting {args.client}...')
        send_ctrl_msg(args.command, args.client)
    elif args.command == 'stop':
        print(f'stopping {args.client}...')
        send_ctrl_msg(args.command, args.client)
    elif args.command == 'status':
        query_status()


if __name__ == '__main__':
    main()

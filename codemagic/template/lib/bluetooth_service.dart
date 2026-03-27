import 'dart:async';
import 'dart:convert';

import 'package:flutter_blue_plus/flutter_blue_plus.dart';

/// Handles WemobBridge bluetooth.* method calls using BLE.
class BluetoothBridgeService {
  final Map<String, BluetoothDevice> _connectedDevices = {};

  /// Routes a bridge method call to the appropriate handler.
  Future<Map<String, dynamic>> handleMethod(
      String method, Map<String, dynamic> args) async {
    try {
      switch (method) {
        case 'bluetooth.isAvailable':
          return await _isAvailable();
        case 'bluetooth.scan':
          return await _scan(args);
        case 'bluetooth.connect':
          return await _connect(args['deviceId']?.toString());
        case 'bluetooth.disconnect':
          return await _disconnect(args['deviceId']?.toString());
        case 'bluetooth.getServices':
          return await _getServices(args['deviceId']?.toString());
        case 'bluetooth.read':
          return await _read(args);
        case 'bluetooth.write':
          return await _write(args);
        default:
          return {'error': 'Unknown bluetooth method: $method'};
      }
    } catch (e) {
      return {'error': e.toString()};
    }
  }

  Future<Map<String, dynamic>> _isAvailable() async {
    final supported = await FlutterBluePlus.isSupported;
    final adapterState = FlutterBluePlus.adapterStateNow;
    return {
      'available': supported,
      'enabled': adapterState == BluetoothAdapterState.on,
    };
  }

  Future<Map<String, dynamic>> _scan(Map<String, dynamic> args) async {
    final duration = (args['duration'] as num?)?.toInt() ?? 5;
    List<Guid> withServices = [];
    if (args['serviceUuids'] is List) {
      withServices =
          (args['serviceUuids'] as List).map((s) => Guid(s.toString())).toList();
    }

    await FlutterBluePlus.startScan(
      withServices: withServices,
      timeout: Duration(seconds: duration),
    );

    // Wait for scan to complete
    await FlutterBluePlus.isScanning.where((val) => val == false).first;

    final results = FlutterBluePlus.lastScanResults;
    final devices = results.map((r) {
      return {
        'id': r.device.remoteId.str,
        'name': r.device.platformName.isNotEmpty ? r.device.platformName : null,
        'rssi': r.rssi,
      };
    }).toList();

    return {'devices': devices};
  }

  Future<Map<String, dynamic>> _connect(String? deviceId) async {
    if (deviceId == null || deviceId.isEmpty) {
      return {'error': 'deviceId is required'};
    }
    final device = BluetoothDevice.fromId(deviceId);
    await device.connect(timeout: const Duration(seconds: 10));
    _connectedDevices[deviceId] = device;
    return {'connected': true};
  }

  Future<Map<String, dynamic>> _disconnect(String? deviceId) async {
    if (deviceId == null || deviceId.isEmpty) {
      return {'error': 'deviceId is required'};
    }
    final device =
        _connectedDevices.remove(deviceId) ?? BluetoothDevice.fromId(deviceId);
    await device.disconnect();
    return {'disconnected': true};
  }

  Future<Map<String, dynamic>> _getServices(String? deviceId) async {
    if (deviceId == null || deviceId.isEmpty) {
      return {'error': 'deviceId is required'};
    }
    final device =
        _connectedDevices[deviceId] ?? BluetoothDevice.fromId(deviceId);
    final services = await device.discoverServices();
    return {
      'services': services.map((s) {
        return {
          'uuid': s.uuid.str,
          'characteristics': s.characteristics.map((c) {
            final props = <String>[];
            if (c.properties.read) props.add('read');
            if (c.properties.write) props.add('write');
            if (c.properties.writeWithoutResponse) {
              props.add('writeWithoutResponse');
            }
            if (c.properties.notify) props.add('notify');
            if (c.properties.indicate) props.add('indicate');
            return {
              'uuid': c.uuid.str,
              'properties': props,
            };
          }).toList(),
        };
      }).toList(),
    };
  }

  Future<Map<String, dynamic>> _read(Map<String, dynamic> args) async {
    final deviceId = args['deviceId']?.toString();
    final serviceUuid = args['serviceUuid']?.toString();
    final characteristicUuid = args['characteristicUuid']?.toString();

    if (deviceId == null || serviceUuid == null || characteristicUuid == null) {
      return {
        'error': 'deviceId, serviceUuid, and characteristicUuid are required'
      };
    }

    final char = await _findCharacteristic(deviceId, serviceUuid, characteristicUuid);
    if (char == null) {
      return {'error': 'characteristic not found'};
    }
    final value = await char.read();
    return {'value': base64Encode(value)};
  }

  Future<Map<String, dynamic>> _write(Map<String, dynamic> args) async {
    final deviceId = args['deviceId']?.toString();
    final serviceUuid = args['serviceUuid']?.toString();
    final characteristicUuid = args['characteristicUuid']?.toString();
    final value = args['value']?.toString();

    if (deviceId == null ||
        serviceUuid == null ||
        characteristicUuid == null ||
        value == null) {
      return {
        'error':
            'deviceId, serviceUuid, characteristicUuid, and value are required'
      };
    }

    final char = await _findCharacteristic(deviceId, serviceUuid, characteristicUuid);
    if (char == null) {
      return {'error': 'characteristic not found'};
    }
    final bytes = base64Decode(value);
    await char.write(bytes);
    return {'success': true};
  }

  Future<BluetoothCharacteristic?> _findCharacteristic(
      String deviceId, String serviceUuid, String characteristicUuid) async {
    final device =
        _connectedDevices[deviceId] ?? BluetoothDevice.fromId(deviceId);
    final services = await device.discoverServices();
    for (final service in services) {
      if (service.uuid.str.toLowerCase() == serviceUuid.toLowerCase()) {
        for (final char in service.characteristics) {
          if (char.uuid.str.toLowerCase() ==
              characteristicUuid.toLowerCase()) {
            return char;
          }
        }
      }
    }
    return null;
  }

  void dispose() {
    for (final device in _connectedDevices.values) {
      device.disconnect();
    }
    _connectedDevices.clear();
  }
}

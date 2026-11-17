import { describe, expect, it } from 'vitest'
import { detectDangerousCommand } from '../dangerousCommand'

describe('detectDangerousCommand', () => {
  describe('recursive delete', () => {
    it.each([
      'rm -rf /',
      'rm -rf ~',
      'rm -rf *',
      'rm -rf .',
      'rm -rf ..',
      'rm -rf /*',
      'rm -fr /',
      'rm -r -f /',
      'rm --recursive --force ~',
      'rm -rf --no-preserve-root /tmp',
    ])('flags %s', (line) => {
      expect(detectDangerousCommand(line)).toBe('recursive-delete')
    })

    it.each([
      'rm -rf build/',
      'rm -rf ./dist',
      'rm -rf /tmp/foo',
      'rm -rf node_modules',
      'rm file.txt',
      'rm -r some-dir',
      'rm --help',
    ])('allows %s', (line) => {
      expect(detectDangerousCommand(line)).toBeNull()
    })
  })

  describe('wrappers', () => {
    it('looks through sudo', () => {
      expect(detectDangerousCommand('sudo rm -rf /')).toBe('recursive-delete')
    })

    it('looks through sudo with a value-taking option', () => {
      expect(detectDangerousCommand('sudo -u root rm -rf /')).toBe('recursive-delete')
    })

    it('looks through leading assignments', () => {
      expect(detectDangerousCommand('LC_ALL=C rm -rf /')).toBe('recursive-delete')
    })
  })

  describe('disk writes', () => {
    it('flags writing to a raw device with dd', () => {
      expect(detectDangerousCommand('dd if=/tmp/img.iso of=/dev/sda')).toBe('disk-write')
    })

    it('allows dd to a regular file', () => {
      expect(detectDangerousCommand('dd if=/dev/zero of=/tmp/img bs=1M count=10')).toBeNull()
    })

    it('flags a redirect into a raw device', () => {
      expect(detectDangerousCommand('cat x > /dev/nvme0n1')).toBe('disk-write')
    })

    it('flags mkfs', () => {
      expect(detectDangerousCommand('mkfs.ext4 /dev/sdb1')).toBe('mkfs')
      expect(detectDangerousCommand('sudo mkfs -t ext4 /dev/sdb1')).toBe('mkfs')
    })
  })

  it('flags the fork bomb', () => {
    expect(detectDangerousCommand(':(){ :|:& };:')).toBe('fork-bomb')
  })

  describe('force push', () => {
    it.each(['git push --force', 'git push -f', 'git push origin main -f'])(
      'flags %s',
      (line) => {
        expect(detectDangerousCommand(line)).toBe('force-push')
      },
    )

    it.each(['git push', 'git push origin main', 'git push --force-with-lease'])(
      'allows %s',
      (line) => {
        expect(detectDangerousCommand(line)).toBeNull()
      },
    )
  })

  describe('power', () => {
    it.each(['shutdown -h now', 'reboot', 'sudo halt', 'poweroff', 'init 0'])(
      'flags %s',
      (line) => {
        expect(detectDangerousCommand(line)).toBe('power')
      },
    )

    it('allows init 3', () => {
      expect(detectDangerousCommand('init 3')).toBeNull()
    })
  })

  describe('dropping data', () => {
    it('flags a statement typed at a database prompt', () => {
      expect(detectDangerousCommand('DROP DATABASE prod')).toBe('drop-data')
      expect(detectDangerousCommand('drop table users;')).toBe('drop-data')
    })

    it('flags a statement passed to a database client', () => {
      expect(detectDangerousCommand('mysql -e "DROP DATABASE prod"')).toBe('drop-data')
      expect(detectDangerousCommand('psql -c "drop table users"')).toBe('drop-data')
    })

    it('allows mentioning it in text', () => {
      expect(detectDangerousCommand('echo "drop table"')).toBeNull()
      expect(detectDangerousCommand('grep -r "DROP TABLE" .')).toBeNull()
    })
  })

  describe('chained commands', () => {
    it('finds the danger in the second segment', () => {
      expect(detectDangerousCommand('ls && rm -rf /')).toBe('recursive-delete')
      expect(detectDangerousCommand('true; shutdown -h now')).toBe('power')
    })

    it('allows a harmless chain', () => {
      expect(detectDangerousCommand('cd /tmp && ls -la')).toBeNull()
    })
  })

  it('ignores empty input', () => {
    expect(detectDangerousCommand('')).toBeNull()
    expect(detectDangerousCommand('   ')).toBeNull()
  })

  it('does not look inside quoted arguments', () => {
    expect(detectDangerousCommand('echo "rm -rf /"')).toBeNull()
  })
})

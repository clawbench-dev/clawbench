import { abilityAccessCtrl, common, Permissions } from '@kit.AbilityKit';
import { MediaAccessGate } from '../models/MediaAccess';

export class MediaPermissionService {
  private context: common.UIAbilityContext;
  private gate: MediaAccessGate = new MediaAccessGate();

  constructor(context: common.UIAbilityContext) {
    this.context = context;
  }

  request(request: PermissionRequest, activeOrigin: string, isCurrent: () => boolean): Promise<void> {
    return this.gate.request(request, activeOrigin, isCurrent, async (names: string[]): Promise<boolean> => {
      const permissions = names as Permissions[];
      const result = await abilityAccessCtrl.createAtManager().requestPermissionsFromUser(this.context, permissions);
      return result.authResults.length === permissions.length &&
        result.authResults.every((status: number): boolean => status === abilityAccessCtrl.GrantStatus.PERMISSION_GRANTED);
    });
  }

  cancel(): void {
    this.gate.cancel();
  }
}

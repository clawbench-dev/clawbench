import { abilityAccessCtrl, common } from '@kit.AbilityKit';
import { pasteboard } from '@kit.BasicServicesKit';

export class ClipboardService {
  private context: common.UIAbilityContext;

  constructor(context: common.UIAbilityContext) {
    this.context = context;
  }

  async read(isCurrent: () => boolean): Promise<string> {
    const result = await abilityAccessCtrl.createAtManager().requestPermissionsFromUser(
      this.context, ['ohos.permission.READ_PASTEBOARD']);
    if (!isCurrent() || result.authResults[0] !== abilityAccessCtrl.GrantStatus.PERMISSION_GRANTED) {
      throw new Error('Clipboard permission was not granted');
    }
    const data = await pasteboard.getSystemPasteboard().getData();
    if (!isCurrent()) {
      throw new Error('Page closed');
    }
    return data.getPrimaryText().slice(0, 1024 * 1024);
  }

  async write(text: string): Promise<void> {
    if (typeof text !== 'string' || text.length > 1024 * 1024) {
      throw new Error('Clipboard text is too large');
    }
    await pasteboard.getSystemPasteboard().setData(pasteboard.createPlainTextData(text));
  }
}

import { common } from '@kit.AbilityKit';
import { camera, cameraPicker } from '@kit.CameraKit';
import { fileIo, fileUri, picker } from '@kit.CoreFileKit';
import { photoAccessHelper } from '@kit.MediaLibraryKit';
import { util } from '@kit.ArkTS';
import { filePickerPlan, FilePickerPlan, FileSelectionGate } from '../models/FilePickerPolicy';

export class FilePickerService {
  private context: common.UIAbilityContext;
  private gate: FileSelectionGate = new FileSelectionGate();
  private capturedFiles: string[] = [];
  private epoch: number = 0;

  constructor(context: common.UIAbilityContext) {
    this.context = context;
  }

  async choose(event: OnShowFileSelectorEvent, isCurrent: () => boolean): Promise<void> {
    const mode = event.fileSelector.getMode();
    if (mode !== FileSelectorMode.FileOpenMode && mode !== FileSelectorMode.FileOpenMultipleMode) {
      event.result.handleFileList([]);
      return;
    }
    const plan = filePickerPlan(event.fileSelector.getAcceptType(), event.fileSelector.isCapture(),
      mode === FileSelectorMode.FileOpenMultipleMode);
    await this.gate.select(event.result, isCurrent, (): Promise<string[]> => this.pick(plan, isCurrent),
      plan.maxSelectNumber);
  }

  cancel(): void {
    this.epoch++;
    this.gate.cancel();
    const files = this.capturedFiles.splice(0);
    files.forEach((path: string): void => {
      fileIo.unlink(path).catch(() => {});
    });
  }

  private async pick(plan: FilePickerPlan, isCurrent: () => boolean): Promise<string[]> {
    if (plan.kind === 'camera') {
      return this.capture(plan, isCurrent);
    }
    if (plan.kind === 'photos') {
      const options = new photoAccessHelper.PhotoSelectOptions();
      options.MIMEType = plan.images && plan.videos ? photoAccessHelper.PhotoViewMIMETypes.IMAGE_VIDEO_TYPE :
        plan.videos ? photoAccessHelper.PhotoViewMIMETypes.VIDEO_TYPE : photoAccessHelper.PhotoViewMIMETypes.IMAGE_TYPE;
      options.maxSelectNumber = plan.maxSelectNumber;
      const result = await new photoAccessHelper.PhotoViewPicker().select(options);
      return result.photoUris;
    }
    const options = new picker.DocumentSelectOptions();
    options.maxSelectNumber = plan.maxSelectNumber;
    if (plan.suffixFilters.length > 0) {
      options.fileSuffixFilters = plan.suffixFilters;
    }
    return new picker.DocumentViewPicker(this.context).select(options);
  }

  private async capture(plan: FilePickerPlan, isCurrent: () => boolean): Promise<string[]> {
    const epoch = this.epoch;
    // A single requested media type keeps the sandbox file extension consistent with the camera output.
    const video = plan.videos && !plan.images;
    const path = this.context.cacheDir + '/capture-' + util.generateRandomUUID(true) + (video ? '.mp4' : '.jpg');
    const file = await fileIo.open(path, fileIo.OpenMode.CREATE | fileIo.OpenMode.READ_WRITE | fileIo.OpenMode.TRUNC);
    await fileIo.close(file);
    try {
      if (epoch !== this.epoch || !isCurrent()) {
        return [];
      }
      const uri = fileUri.getUriFromPath(path);
      const profile: cameraPicker.PickerProfile = {
        cameraPosition: camera.CameraPosition.CAMERA_POSITION_BACK,
        saveUri: uri
      };
      const result = await cameraPicker.pick(this.context,
        [video ? cameraPicker.PickerMediaType.VIDEO : cameraPicker.PickerMediaType.PHOTO], profile);
      if (result.resultCode !== 0 || result.resultUri !== uri || epoch !== this.epoch || !isCurrent()) {
        return [];
      }
      this.capturedFiles.push(path);
      return [uri];
    } finally {
      if (!this.capturedFiles.includes(path)) {
        await fileIo.unlink(path).catch(() => {});
      }
    }
  }
}

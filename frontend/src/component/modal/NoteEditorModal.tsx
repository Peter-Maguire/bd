import { useCallback, useState } from 'react';
import Dialog from '@mui/material/Dialog';
import {
    DialogActions,
    DialogContent,
    DialogTitle,
    TextField
} from '@mui/material';
import Stack from '@mui/material/Stack';
import { Trans, useTranslation } from 'react-i18next';
import { saveUserNote } from '../../api.ts';
import NiceModal, { muiDialog, useModal } from '@ebay/nice-modal-react';
import { logError } from '../../util.ts';
import CancelButton from '../CancelButton.tsx';
import SaveButton from '../SaveButton.tsx';
import ClearButton from '../ClearButton.tsx';

interface NoteEditorProps {
    notes: string;
    steamId: string;
}

export const NoteEditorModal = NiceModal.create<NoteEditorProps>(
    ({ steamId, notes }) => {
        const [newNotes, setNewNotes] = useState<string>(notes);
        const { t } = useTranslation();
        const modal = useModal();

        const onSaveNotes = useCallback(async () => {
            try {
                await saveUserNote(steamId, newNotes);
                await modal.hide();
            } catch (e) {
                logError(`Error updating note: ${e}`);
            }
        }, [newNotes, steamId, modal]);

        return (
            <Dialog fullWidth {...muiDialog(modal)}>
                <DialogTitle>
                    <Trans i18nKey={'player_table.notes.title'} />
                </DialogTitle>
                <DialogContent>
                    <Stack spacing={1} padding={0}>
                        <TextField
                            id="notes-editor-field"
                            label={t('player_table.notes.note_label')}
                            fullWidth
                            minRows={10}
                            value={newNotes}
                            onChange={(evt) => {
                                setNewNotes(evt.target.value);
                            }}
                            multiline
                        />
                    </Stack>
                </DialogContent>
                <DialogActions>
                    <ClearButton
                        onClick={() => {
                            setNewNotes('');
                        }}
                    />
                    <CancelButton onClick={modal.hide} />
                    <SaveButton onClick={onSaveNotes} />
                </DialogActions>
            </Dialog>
        );
    }
);

export default NoteEditorModal;

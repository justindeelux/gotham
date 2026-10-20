import{b0 as O,gW as G,gX as u,fQ as g,b2 as I,bT as l,bS as k,fu as Q,b1 as X,d as Y,b3 as W,o as t,p as C,fw as q,b6 as J,bK as U,bY as Z,f as H,j as ee,c as T,b5 as h,bG as oe,bC as z,e8 as re,e9 as ne,fE as te,fD as le,fC as se,fF as ie,a as ae,e6 as ce,b4 as A,fp as de,b8 as a}from"./index-BKBiHcrk.js";function fe(e){const{lineHeight:s,borderRadius:v,fontWeightStrong:p,baseColor:r,dividerColor:_,actionColor:E,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:b,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:S,fontSize:$}=e;return{...G,fontSize:$,lineHeight:s,titleFontWeight:p,borderRadius:v,border:`1px solid ${_}`,color:E,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:v,closeColorHover:f,closeColorPressed:b,closeIconColor:m,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${u(r,g(o,{alpha:.25}))}`,colorInfo:u(r,g(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:b,closeIconColorInfo:m,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${u(r,g(y,{alpha:.25}))}`,colorSuccess:u(r,g(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:b,closeIconColorSuccess:m,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${u(r,g(P,{alpha:.33}))}`,colorWarning:u(r,g(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:b,closeIconColorWarning:m,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${u(r,g(S,{alpha:.25}))}`,colorError:u(r,g(S,{alpha:.08})),titleTextColorError:d,iconColorError:S,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:b,closeIconColorError:m,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ue={common:O,self:fe};var ge=I("alert",`
 line-height: var(--n-line-height);
 border-radius: var(--n-border-radius);
 position: relative;
 transition: background-color .3s var(--n-bezier);
 background-color: var(--n-color);
 text-align: start;
 word-break: break-word;
`,[l("border",`
 border-radius: inherit;
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 transition: border-color .3s var(--n-bezier);
 border: var(--n-border);
 pointer-events: none;
 `),k("closable",[I("alert-body",[l("title",`
 padding-right: 24px;
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),Q({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
 position: absolute;
 left: 0;
 top: 0;
 align-items: center;
 justify-content: center;
 display: flex;
 width: var(--n-icon-size);
 height: var(--n-icon-size);
 font-size: var(--n-icon-size);
 margin: var(--n-icon-margin);
 `),l("close",`
 transition:
 color .3s var(--n-bezier),
 background-color .3s var(--n-bezier);
 position: absolute;
 right: 0;
 top: 0;
 margin: var(--n-close-margin);
 `),k("show-icon",[I("alert-body",{paddingLeft:"calc(var(--n-icon-margin-left) + var(--n-icon-size) + var(--n-icon-margin-right))"})]),k("right-adjust",[I("alert-body",{paddingRight:"calc(var(--n-close-size) + var(--n-padding) + 2px)"})]),I("alert-body",`
 border-radius: var(--n-border-radius);
 transition: border-color .3s var(--n-bezier);
 `,[l("title",`
 transition: color .3s var(--n-bezier);
 font-size: 16px;
 line-height: 19px;
 font-weight: var(--n-title-font-weight);
 `,[X("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const he={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var Ce=Y({name:"Alert",inheritAttrs:!1,props:he,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:v,inlineThemeDisabled:p,mergedRtlRef:r}=J(e),_=W("Alert","-alert",ge,ue,e,s),E=U("Alert",r,s),d=H(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=_.value,{fontSize:y,borderRadius:P,titleFontWeight:S,lineHeight:$,iconSize:w,iconMargin:R,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:j,closeMarginRtl:V,padding:D}=o,{type:i}=e,{left:K,right:N}=de(R);return{"--n-bezier":n,"--n-color":o[a("color",i)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[a("closeColorHover",i)],"--n-close-color-pressed":o[a("closeColorPressed",i)],"--n-close-icon-color":o[a("closeIconColor",i)],"--n-close-icon-color-hover":o[a("closeIconColorHover",i)],"--n-close-icon-color-pressed":o[a("closeIconColorPressed",i)],"--n-icon-color":o[a("iconColor",i)],"--n-border":o[a("border",i)],"--n-title-text-color":o[a("titleTextColor",i)],"--n-content-text-color":o[a("contentTextColor",i)],"--n-line-height":$,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":S,"--n-icon-size":w,"--n-icon-margin":R,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":j,"--n-close-margin-rtl":V,"--n-padding":D,"--n-icon-margin-left":K,"--n-icon-margin-right":N}}),c=p?Z("alert",H(()=>e.type[0]),d,e):void 0,f=ee(!0),b=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:E,mergedClsPrefix:s,mergedBordered:v,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{b()},mergedTheme:_,cssVars:p?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),C(q,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,v={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),T("div",A({key:1},A(this.$attrs,v)),[h(()=>this.closable&&(t(),C(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),h(()=>this.bordered&&(t(),T("div",{class:z(`${e}-alert__border`)},null,2))),h(()=>this.showIcon&&(t(),T("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[h(()=>re(s.icon,()=>[(t(),C(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),C(ie,{key:3});case"info":return t(),C(se,{key:4});case"warning":return t(),C(le,{key:5});case"error":return t(),C(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ae("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[h(()=>ce(s.header,p=>{const r=p||this.title;return r?(t(),T("div",{key:2,class:z(`${e}-alert-body__title`)},[h(()=>r)],2)):null})),h(()=>s.default&&(t(),T("div",{class:z(`${e}-alert-body__content`)},[h(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{Ce as A};

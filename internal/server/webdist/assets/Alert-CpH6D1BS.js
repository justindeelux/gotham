import{b5 as G,g_ as J,g$ as u,fU as g,b7 as I,bY as l,bX as k,fy as K,b6 as U,d as X,b8 as W,o as t,p as C,fA as Y,bb as q,bP as Q,c1 as Z,f as R,j as ee,c as S,ba as h,bL as oe,bH as z,ec as re,ed as ne,fI as te,fH as le,fG as se,fJ as ae,a as ie,ea as ce,b9 as A,ft as de,bd as i}from"./index-CU2L4zjd.js";function fe(e){const{lineHeight:s,borderRadius:v,fontWeightStrong:m,baseColor:r,dividerColor:T,actionColor:$,textColor1:d,textColor2:c,closeColorHover:f,closeColorPressed:b,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,infoColor:o,successColor:y,warningColor:P,errorColor:_,fontSize:H}=e;return{...J,fontSize:H,lineHeight:s,titleFontWeight:m,borderRadius:v,border:`1px solid ${T}`,color:$,titleTextColor:d,iconColor:c,contentTextColor:c,closeBorderRadius:v,closeColorHover:f,closeColorPressed:b,closeIconColor:p,closeIconColorHover:x,closeIconColorPressed:n,borderInfo:`1px solid ${u(r,g(o,{alpha:.25}))}`,colorInfo:u(r,g(o,{alpha:.08})),titleTextColorInfo:d,iconColorInfo:o,contentTextColorInfo:c,closeColorHoverInfo:f,closeColorPressedInfo:b,closeIconColorInfo:p,closeIconColorHoverInfo:x,closeIconColorPressedInfo:n,borderSuccess:`1px solid ${u(r,g(y,{alpha:.25}))}`,colorSuccess:u(r,g(y,{alpha:.08})),titleTextColorSuccess:d,iconColorSuccess:y,contentTextColorSuccess:c,closeColorHoverSuccess:f,closeColorPressedSuccess:b,closeIconColorSuccess:p,closeIconColorHoverSuccess:x,closeIconColorPressedSuccess:n,borderWarning:`1px solid ${u(r,g(P,{alpha:.33}))}`,colorWarning:u(r,g(P,{alpha:.08})),titleTextColorWarning:d,iconColorWarning:P,contentTextColorWarning:c,closeColorHoverWarning:f,closeColorPressedWarning:b,closeIconColorWarning:p,closeIconColorHoverWarning:x,closeIconColorPressedWarning:n,borderError:`1px solid ${u(r,g(_,{alpha:.25}))}`,colorError:u(r,g(_,{alpha:.08})),titleTextColorError:d,iconColorError:_,contentTextColorError:c,closeColorHoverError:f,closeColorPressedError:b,closeIconColorError:p,closeIconColorHoverError:x,closeIconColorPressedError:n}}const ue={common:G,self:fe};var ge=I("alert",`
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
 `)])]),l("icon",{color:"var(--n-icon-color)"}),I("alert-body",{padding:"var(--n-padding)"},[l("title",{color:"var(--n-title-text-color)"}),l("content",{color:"var(--n-content-text-color)"})]),K({originalTransition:"transform .3s var(--n-bezier)",enterToProps:{transform:"scale(1)"},leaveToProps:{transform:"scale(0.9)"}}),l("icon",`
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
 `,[U("& +",[l("content",{marginTop:"9px"})])]),l("content",{transition:"color .3s var(--n-bezier)",fontSize:"var(--n-font-size)"})]),l("icon",{transition:"color .3s var(--n-bezier)"})]);const he={...W.props,title:String,showIcon:{type:Boolean,default:!0},type:{type:String,default:"default"},bordered:{type:Boolean,default:!0},closable:Boolean,onClose:Function,onAfterLeave:Function,onAfterHide:Function};var Ce=X({name:"Alert",inheritAttrs:!1,props:he,slots:Object,setup(e){const{mergedClsPrefixRef:s,mergedBorderedRef:v,inlineThemeDisabled:m,mergedRtlRef:r}=q(e),T=W("Alert","-alert",ge,ue,e,s),$=Q("Alert",r,s),d=R(()=>{const{common:{cubicBezierEaseInOut:n},self:o}=T.value,{fontSize:y,borderRadius:P,titleFontWeight:_,lineHeight:H,iconSize:w,iconMargin:E,iconMarginRtl:B,closeIconSize:L,closeBorderRadius:F,closeSize:M,closeMargin:j,closeMarginRtl:V,padding:N}=o,{type:a}=e,{left:O,right:D}=de(E);return{"--n-bezier":n,"--n-color":o[i("color",a)],"--n-close-icon-size":L,"--n-close-border-radius":F,"--n-close-color-hover":o[i("closeColorHover",a)],"--n-close-color-pressed":o[i("closeColorPressed",a)],"--n-close-icon-color":o[i("closeIconColor",a)],"--n-close-icon-color-hover":o[i("closeIconColorHover",a)],"--n-close-icon-color-pressed":o[i("closeIconColorPressed",a)],"--n-icon-color":o[i("iconColor",a)],"--n-border":o[i("border",a)],"--n-title-text-color":o[i("titleTextColor",a)],"--n-content-text-color":o[i("contentTextColor",a)],"--n-line-height":H,"--n-border-radius":P,"--n-font-size":y,"--n-title-font-weight":_,"--n-icon-size":w,"--n-icon-margin":E,"--n-icon-margin-rtl":B,"--n-close-size":M,"--n-close-margin":j,"--n-close-margin-rtl":V,"--n-padding":N,"--n-icon-margin-left":O,"--n-icon-margin-right":D}}),c=m?Z("alert",R(()=>e.type[0]),d,e):void 0,f=ee(!0),b=()=>{const{onAfterLeave:n,onAfterHide:o}=e;n&&n(),o&&o()};return{rtlEnabled:$,mergedClsPrefix:s,mergedBordered:v,visible:f,handleCloseClick:()=>{Promise.resolve(e.onClose?.()).then(n=>{n!==!1&&(f.value=!1)})},handleAfterLeave:()=>{b()},mergedTheme:T,cssVars:m?void 0:d,themeClass:c?.themeClass,onRender:c?.onRender}},render(){return this.onRender?.(),t(),C(Y,{onAfterLeave:this.handleAfterLeave},{default:()=>{const{mergedClsPrefix:e,$slots:s}=this,v={class:[`${e}-alert`,this.themeClass,this.closable&&`${e}-alert--closable`,this.showIcon&&`${e}-alert--show-icon`,!this.title&&this.closable&&`${e}-alert--right-adjust`,this.rtlEnabled&&`${e}-alert--rtl`],style:this.cssVars,role:"alert"};return this.visible?(t(),S("div",A({key:1},A(this.$attrs,v)),[h(()=>this.closable&&(t(),C(oe,{clsPrefix:e,class:z(`${e}-alert__close`),onClick:this.handleCloseClick},null,8,["clsPrefix","class","onClick"]))),h(()=>this.bordered&&(t(),S("div",{class:z(`${e}-alert__border`)},null,2))),h(()=>this.showIcon&&(t(),S("div",{class:z(`${e}-alert__icon`),"aria-hidden":"true"},[h(()=>re(s.icon,()=>[(t(),C(ne,{clsPrefix:e},{default:()=>{switch(this.type){case"success":return t(),C(ae,{key:3});case"info":return t(),C(se,{key:4});case"warning":return t(),C(le,{key:5});case"error":return t(),C(te,{key:6});default:return null}}},1032,["clsPrefix"]))]))],2))),ie("div",{class:z([`${e}-alert-body`,this.mergedBordered&&`${e}-alert-body--bordered`])},[h(()=>ce(s.header,m=>{const r=m||this.title;return r?(t(),S("div",{key:2,class:z(`${e}-alert-body__title`)},[h(()=>r)],2)):null})),h(()=>s.default&&(t(),S("div",{class:z(`${e}-alert-body__content`)},[h(()=>s.default())],2)))],2)],16)):null}},1032,["onAfterLeave"])}});export{Ce as A};
